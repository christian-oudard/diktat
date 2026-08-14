// Daemon: keeps the model loaded, toggles recording on SIGUSR1, transcribes
// on stop, types the result.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/christian-oudard/diktat/internal/asr"
	"github.com/christian-oudard/diktat/internal/audio"
	"github.com/christian-oudard/diktat/internal/config"
	"github.com/christian-oudard/diktat/internal/human"
	"github.com/christian-oudard/diktat/internal/ipc"
	"github.com/christian-oudard/diktat/internal/models"
	"github.com/christian-oudard/diktat/internal/output"
	"github.com/christian-oudard/diktat/internal/wav"
)

const (
	statusLoad = `<span color="#fabd2f">● LOAD</span>`
	statusRec  = `<span color="#fb4934">● REC</span>`
	statusTx   = `<span color="#458588">● TX</span>`
)

func runDaemon(args []string) {
	if len(args) > 0 {
		log.Fatalf("daemon takes no arguments; set model in %s", config.DefaultPath())
	}

	// Install handlers before loading the model: until this runs, SIGUSR1
	// keeps its default disposition and would kill the daemon. A toggle
	// pressed during startup queues here instead.
	sigCh := make(chan os.Signal, 8)
	signal.Notify(sigCh, syscall.SIGUSR1, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)

	// Pre-flight before the log is redirected to a file, so a user who runs
	// this in a terminal sees why it would not start.
	cfg, unknown, err := config.Load(config.DefaultPath())
	if err != nil {
		log.Printf("config: %v (continuing with defaults)", err)
		cfg = &config.Config{}
	}
	for _, key := range unknown {
		// Silence here is how vocabulary_hints sat in a real config doing
		// nothing for two months.
		log.Printf("config: ignoring unknown key %q", key)
	}
	// Nothing is bundled, and nothing is downloaded implicitly: say what to
	// type instead.
	name := config.StartModel()
	modelDir := models.Resolve(name)
	if err := models.Check(modelDir); err != nil {
		log.Fatalf("%s is not downloaded. Get it with:\n  diktat model %s", name, name)
	}

	if logf, err := os.OpenFile(ipc.LogFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err == nil {
		log.SetOutput(logf)
	}
	log.SetFlags(log.Ltime)
	log.Printf("Starting %s %s", gitRev, exePath())

	if err := os.WriteFile(ipc.PIDFile, []byte(fmt.Sprint(os.Getpid())), 0644); err != nil {
		log.Fatalf("write pid: %v", err)
	}
	defer os.Remove(ipc.PIDFile)
	defer os.Remove(ipc.StatusFile)
	setStatus(statusLoad)

	// A `diktat model` switch deliberately does not persist: the daemon comes
	// back on the configured model rather than on whatever a stale /tmp file
	// says.
	model, err := asr.Load(modelDir)
	if err != nil {
		log.Fatalf("load model: %v", err)
	}
	defer os.Remove(ipc.ModelFile)

	rec, err := audio.NewRecorder()
	if err != nil {
		log.Fatalf("audio recorder: %v", err)
	}
	defer rec.Close()

	d := &daemon{
		model:    model,
		models:   map[string]*asr.Model{modelDir: model},
		lru:      []string{modelDir},
		budget:   cacheBudget(cfg),
		recorder: rec,
		cfg:      cfg,
		modelDir: modelDir,
		loaded:   make(chan loadResult, 1),
	}
	defer d.closeModels()
	warm(model)
	d.applyVocabulary(model)
	d.publishModel()
	log.Printf("Model loaded: %s", model.Arch())
	setStatus("")

	for {
		select {
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGUSR1:
				if d.isRecording() {
					d.stopRecording()
				} else {
					d.startRecording()
				}
			case syscall.SIGHUP:
				d.requestModel()
			case syscall.SIGTERM, syscall.SIGINT:
				d.mu.Lock()
				d.recording = false
				d.mu.Unlock()
				log.Println("Daemon stopped.")
				return
			}
		case res := <-d.loaded:
			d.finishLoad(res)
		}
	}
}

type daemon struct {
	model *asr.Model
	// Every model loaded this session, kept resident so switching back to one
	// already seen is instant.
	models map[string]*asr.Model
	// lru is the model cache's use order, oldest first, and budget is what
	// the cache may hold. Together they bound resident memory, which on a
	// laptop GPU is the scarce resource.
	lru       []string
	budget    uint64
	recorder  *audio.Recorder
	cfg       *config.Config
	startedAt time.Time
	modelDir  string

	// loaded carries a model loaded off the main loop back to it, since that
	// loop owns every field here. loading is what is being loaded now, and
	// wanted is a model asked for while that was still in flight.
	loaded  chan loadResult
	loading string
	wanted  string

	mu        sync.Mutex
	recording bool
}

// loadResult is a finished background load, successful or not.
type loadResult struct {
	dir   string
	model *asr.Model
	err   error
	took  time.Duration
}

func (d *daemon) publishModel() {
	if err := os.WriteFile(ipc.ModelFile, []byte(d.modelDir), 0644); err != nil {
		log.Printf("model publish: %v", err)
	}
}

// requestModel acts on the model named in ipc.ModelFile. That file is a
// request on the way in and a statement of fact on the way out, so it is put
// back to the model actually loaded straight away: a switch is not a switch
// until the new model can transcribe, and a 2 GB model takes tens of seconds
// to get there.
func (d *daemon) requestModel() {
	raw, err := os.ReadFile(ipc.ModelFile)
	if err != nil {
		log.Printf("model request: %v", err)
		return
	}
	dir := strings.TrimSpace(string(raw))
	d.publishModel()
	d.switchTo(dir)
}

// switchTo installs a model that is already resident, or starts fetching one
// that is not. The fetch runs off the main loop, because the daemon's whole
// job is to answer a keypress: waiting for a load here would mean pressing to
// talk and getting nothing until it finished. The old model keeps serving in
// the meantime, and the loaded model arrives back on d.loaded, which is read
// by the same loop that owns every field here.
//
// The cost of that: a transcription started mid-load shares the card with the
// load, so it is slower than it would be alone, and asr.Load measures a
// model's size from the device's free memory either side of it, which a
// concurrent transcription inflates. Both are worth a responsive keypress.
func (d *daemon) switchTo(dir string) {
	if dir == d.modelDir {
		log.Printf("Already using %s", dir)
		return
	}
	if model, ok := d.models[dir]; ok {
		d.install(dir, model)
		log.Printf("Model now %s, already resident", model.Arch())
		return
	}
	if dir == d.loading {
		// Asking twice for the model already on its way. Worth its own case:
		// the published file names the model still in use, so a second ask
		// looks new, and queueing it would load nothing and then switch to
		// what had just been switched to.
		log.Printf("Already loading %s", dir)
		return
	}
	if d.loading != "" {
		// One load at a time: two at once would compete for the same card,
		// and the second ask is the one that will be honoured anyway.
		d.wanted = dir
		log.Printf("Queued %s behind the load in flight", dir)
		return
	}
	d.loading = dir
	d.restoreStatus()
	log.Printf("Loading %s in the background", dir)
	go func() {
		t0 := time.Now()
		model, err := asr.Load(dir)
		if err == nil {
			// Reading a couple of gigabytes off disk and warming it are
			// separate costs with separate causes, and a switch that is
			// taking too long is a question about which one. Warmed says
			// what the ladder cost; this says what came before it.
			log.Printf("Loaded %s in %s, warming", model.Name(),
				time.Since(t0).Round(time.Millisecond))
			// Warm first, then hand over the hints: an instructed audio-LLM
			// told to expect terms and then given warm audio has been seen
			// to invent its way to the decode budget.
			warm(model)
			d.applyVocabulary(model)
		}
		d.loaded <- loadResult{dir: dir, model: model, err: err, took: time.Since(t0)}
	}()
}

// finishLoad installs what the background load produced, and then honours a
// switch asked for while it was running.
func (d *daemon) finishLoad(res loadResult) {
	d.loading = ""
	if res.err != nil {
		// Keep serving with the model we have. The published model still
		// names it, since a request never overwrote it.
		log.Printf("model load %s: %v", res.dir, res.err)
	} else {
		d.models[res.dir] = res.model
		d.install(res.dir, res.model)
		log.Printf("Model now %s in %s, %d resident, %s cached",
			res.model.Arch(), res.took.Round(time.Millisecond),
			len(d.models), human.Bytes(d.cached()))
	}
	// A request made during the load, unless it asked for what the load just
	// installed, which is what asking twice for a slow model looks like.
	next := d.wanted
	d.wanted = ""
	if next != "" && next != d.modelDir {
		d.switchTo(next)
	}
}

// install makes a resident model the one in use. Recording is not
// interrupted: the capture buffer is independent of the model, so a swap
// while armed just means the new model transcribes what was captured.
func (d *daemon) install(dir string, model *asr.Model) {
	d.model, d.modelDir = model, dir
	d.touch(dir)
	d.evict()
	d.publishModel()
	d.restoreStatus()
}

// applyVocabulary hands the configured hints to a freshly loaded model, and
// says so when the family cannot use them: a list that is quietly ignored is
// worse than no list, since it looks like it is working.
func (d *daemon) applyVocabulary(m *asr.Model) {
	if d.cfg.VocabularyHints == "" {
		return
	}
	if !m.TakesVocabulary() {
		log.Printf("Vocabulary hints ignored: %s takes no initial prompt", m.Name())
		return
	}
	m.SetVocabulary(d.cfg.VocabularyHints)
	log.Printf("Vocabulary hints applied (%d chars)", len(d.cfg.VocabularyHints))
}

// touch records dir as the most recently used model.
func (d *daemon) touch(dir string) {
	d.lru = slices.DeleteFunc(d.lru, func(s string) bool { return s == dir })
	d.lru = append(d.lru, dir)
}

// cached is what every resident model costs together.
func (d *daemon) cached() uint64 {
	var total uint64
	for _, m := range d.models {
		total += m.Bytes()
	}
	return total
}

// overBudget is which models to drop, oldest first, to bring a cache holding
// `sizes` within budget. The last entry in lru is the one in use and is never
// returned, so a budget too small even for one model degrades to keeping
// exactly that one rather than to keeping none.
//
// Split from evict so the policy can be tested without loading a model.
func overBudget(lru []string, sizes map[string]uint64, budget uint64) []string {
	var total uint64
	for _, s := range sizes {
		total += s
	}
	var drop []string
	for i := 0; i+1 < len(lru) && total > budget; i++ {
		drop = append(drop, lru[i])
		total -= sizes[lru[i]]
	}
	return drop
}

// evict frees least-recently-used models until the resident set fits the
// budget. Models stay loaded because switching back is then instant, but on
// a laptop GPU that generosity runs out: the card here has 8 GB, and ggml's
// context and compute buffers cost more than the weights do for a small
// model, so a few switches can fill it.
func (d *daemon) evict() {
	sizes := make(map[string]uint64, len(d.models))
	for dir, m := range d.models {
		sizes[dir] = m.Bytes()
	}
	for _, dir := range overBudget(d.lru, sizes, d.budget) {
		log.Printf("Evicting %s (%s), cache over %s budget",
			dir, human.Bytes(sizes[dir]), human.Bytes(d.budget))
		d.models[dir].Close()
		delete(d.models, dir)
		d.lru = slices.DeleteFunc(d.lru, func(s string) bool { return s == dir })
	}
}

// cacheBudget is how much memory resident models may hold together.
// Configured in MB, or two thirds of the compute device's memory: enough to
// keep a couple of models around, with room left for the rest of the desktop
// on a shared laptop GPU. A device that reports no memory falls back to a
// figure that fits the models in the menu without assuming a big card.
func cacheBudget(cfg *config.Config) uint64 {
	if cfg.ModelCacheMB > 0 {
		return uint64(cfg.ModelCacheMB) << 20
	}
	if total := asr.DeviceMemory(); total > 0 {
		return total / 3 * 2
	}
	return 4 << 30
}

// warm rehearses the ladder of lengths in internal/audio, because loading a
// model is not the same as being ready to use it: the Vulkan backend defers
// compiling its shaders, and ggml defers allocating its compute buffers, to
// the first graph run of each shape, and the shape follows the length of the
// audio. The daemon is resident and loads eagerly, so pay that here rather
// than on the first thing the user says.
//
// The cost is one throwaway transcription per rung, a few hundred
// milliseconds in total once the driver has the shaders on disk, and it runs
// off the main loop either way.
//
// It rehearses on speech rather than on a signal because half the cost is in
// the decoder, whose shapes follow the number of tokens that come out. Noise
// emits none, so under it a 20s utterance on granite still paid 5.3s of
// decode; on speech, 57ms.
//
// There is nothing to warm on the CPU: with no shaders to compile, seven warm
// strategies measured identically to no warmup at all.
func warm(m *asr.Model) {
	if !m.OnGPU() {
		return
	}
	speech, err := warmSpeech()
	if err != nil {
		log.Printf("warmup: %v", err)
		return
	}
	t0 := time.Now()
	rungs := audio.Warm(m.MaxAudio())
	// Per rung rather than a total, since the total cannot say which rung was
	// worth running. A rung that compiles nothing on every model is one to
	// drop, and a rung that compiles on a machine where the ladder was never
	// measured is the ladder being too sparse for that GPU.
	work := make([]string, 0, len(rungs))
	before := m.CompiledKernels()
	for _, secs := range rungs {
		rung := time.Now()
		// A truncated warmup is a success: the graph ran, which is the entire
		// point, and the transcript is thrown away either way. An audio-LLM
		// can talk its way to the decode budget on a rehearsal, and giving up
		// there would leave the cache budgeting the largest models by their
		// file size.
		if _, err := m.Transcribe(fit(speech, secs)); err != nil && !errors.Is(err, asr.ErrTruncated) {
			log.Printf("warmup %ds: %v", secs, err)
			return
		}
		compiled := m.CompiledKernels() - before
		before += compiled
		work = append(work, fmt.Sprintf("%ds:%s/%dk", secs,
			time.Since(rung).Round(time.Millisecond), compiled))
	}
	// Loading allocated the weights; those runs allocated the buffers, which
	// are the larger half and grow with the longest rung. Now is when the
	// model's real cost is knowable.
	m.Measure()
	t := m.Timings()
	log.Printf("Warmed in %s: %s, %s resident (last rung: mel %s, encode %s, decode %s, other %s)",
		time.Since(t0).Round(time.Millisecond), strings.Join(work, " "), human.Bytes(m.Bytes()),
		t.Mel.Round(time.Millisecond), t.Encode.Round(time.Millisecond),
		t.Decode.Round(time.Millisecond), t.Other.Round(time.Millisecond))
}

// warmSentences are the Harvard sentences from docs/mic-calibration.md, which
// are phonetically balanced and already in the repo for the microphone check.
const warmSentences = "The birch canoe slid on the smooth planks. " +
	"Glue the sheet to the dark blue background. " +
	"These days a chicken leg is a rare dish. " +
	"The juice of lemons makes fine punch. " +
	"A pod of whales sped past the quiet cove."

// warmSpeech renders those sentences once and keeps them, since every load
// this session warms on the same audio and the buffer is under 2 MB.
//
// Synthesised rather than shipped: a wav in the repo is data, and the daemon
// only needs audio that provokes a realistic number of tokens. Measured,
// espeak-ng and a neural synthesiser warmed identically, 725ms against 722ms
// on the probe that matters, so the cheap one wins.
var warmSpeech = sync.OnceValues(func() ([]float32, error) {
	// espeak-ng renders at its voice's rate whatever we ask for, so the rate
	// comes back from the header rather than being assumed.
	wave, err := exec.Command("espeak-ng", "-v", "en-us", "-s", "150", "--stdout", warmSentences).Output()
	if err != nil {
		return nil, fmt.Errorf("espeak-ng: %w", err)
	}
	samples, rate, err := wav.Decode(wave)
	if err != nil {
		return nil, err
	}
	return audio.Resample(samples, rate, audio.SampleRate), nil
})

// fit cuts or loops speech to exactly secs of audio. Looping is honest here:
// the rung is about the shape of the graph, and a repeated sentence produces
// tokens at the same rate as a longer one would.
func fit(speech []float32, secs int) []float32 {
	out := make([]float32, secs*audio.SampleRate)
	for i := range out {
		out[i] = speech[i%len(speech)]
	}
	return out
}

func (d *daemon) closeModels() {
	for _, m := range d.models {
		m.Close()
	}
}

// restoreStatus puts the bar back to whatever is true now that whatever it
// was showing is over. A load in the background outranks idle but not
// recording: it is worth knowing a switch is pending, and worth more to know
// the mic is live.
func (d *daemon) restoreStatus() {
	switch {
	case d.isRecording():
		setStatus(statusRec)
	case d.loading != "":
		setStatus(statusLoad)
	default:
		setStatus("")
	}
}

func (d *daemon) isRecording() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.recording
}

func (d *daemon) startRecording() {
	d.startedAt = time.Now()
	d.recorder.Start()
	d.mu.Lock()
	d.recording = true
	d.mu.Unlock()
	setStatus(statusRec)
	log.Println("Recording...")
}

func (d *daemon) stopRecording() {
	samples := d.recorder.Stop()
	d.mu.Lock()
	d.recording = false
	d.mu.Unlock()

	if len(samples) == 0 {
		d.restoreStatus()
		log.Println("No audio.")
		return
	}

	setStatus(statusTx)
	peak, rms := audio.Levels(samples)
	silent := audio.IsSilent(samples)
	// One gain for the whole capture, applied to each piece as it is
	// converted, so a recording split into chunks does not change loudness
	// halfway through.
	gain := audio.Gain(samples)
	// Audio duration is derived from the sample count at the rate we asked the
	// device for. If it drifts from the wall clock, the device is not actually
	// giving us that rate, and the model is seeing time-stretched speech.
	log.Printf("Transcribing %.1fs (wall %.1fs, peak %.3f rms %.4f gain %.1fx)...",
		float64(len(samples))/float64(audio.SampleRate), time.Since(d.startedAt).Seconds(),
		peak, rms, gain)

	// Nothing was said. Every family invents something when asked to
	// transcribe silence, and the inventions cost more than the check does.
	if silent {
		log.Printf("Nothing to transcribe: the capture is silent.")
		setStatus("")
		return
	}

	t0 := time.Now()
	kernels := d.model.CompiledKernelNames()
	// Only what the model will refuse outright gets cut, at the quietest moment
	// near the limit. Most families take the whole utterance and window it
	// themselves, which they do better than a cut here can: cutting at 30s cost
	// a broken sentence at every seam even on models that had no limit at all.
	limit := d.model.MaxAudio()
	chunks := audio.Chunk(samples, int(limit.Seconds())*audio.SampleRate)
	if len(chunks) > 1 {
		log.Printf("Over the model's %s limit, transcribing in %d pieces", limit, len(chunks))
	}
	var parts []string
	for _, chunk := range chunks {
		part, err := d.model.Transcribe(audio.Pad(audio.Floats(chunk, gain)))
		if err != nil {
			log.Printf("transcribe: %v", err)
			d.restoreStatus()
			return
		}
		if part != "" {
			parts = append(parts, part)
		}
	}
	text := strings.Join(parts, " ")
	// Anything compiled here is a shape the warmup did not cover, and it is
	// the reason this transcription was slower than the next one at the same
	// length will be. Expected past the last warm rung, a bug below it, and
	// named either way, since the name says which variant the ladder missed.
	if compiled := d.model.CompiledKernelNames()[len(kernels):]; len(compiled) > 0 {
		log.Printf("Compiled %d kernels mid-transcription, on %.1fs of audio: %s",
			len(compiled), float64(len(samples))/float64(audio.SampleRate),
			strings.Join(compiled, " "))
	}
	// The text itself is deliberately not logged: the log is a long-lived file
	// in /tmp and everything dictated would accumulate in it, which is also
	// why it can stay there. Length is enough to tell "heard nothing" from
	// "heard something" when reading the log.
	// The breakdown separates the model's own work from everything around
	// it, which is what tells a slow model from a cold one: a first
	// utterance that spends its time in encode is still compiling shaders.
	tm := d.model.Timings()
	log.Printf("Transcribed in %s (mel %s, encode %s, decode %s, other %s): %d chars",
		time.Since(t0).Round(time.Millisecond), tm.Mel.Round(time.Millisecond),
		tm.Encode.Round(time.Millisecond), tm.Decode.Round(time.Millisecond),
		tm.Other.Round(time.Millisecond), len(text))

	if text != "" {
		out := text + " "
		if path, err := ipc.LastText(); err != nil {
			log.Printf("last-text: %v", err)
		} else if err := os.WriteFile(path, []byte(out), 0600); err != nil {
			log.Printf("last-text write: %v", err)
		}
		d.appendHistory(text)
		if err := output.Type(out, d.cfg.PasteMethods); err != nil {
			log.Printf("type: %v", err)
		}
	}

	d.restoreStatus()
}

func (d *daemon) appendHistory(text string) {
	if d.cfg.HistoryFile == "" {
		return
	}
	path := d.cfg.HistoryFile
	if path[0] == '~' {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[1:])
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		log.Printf("history mkdir: %v", err)
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("history open: %v", err)
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	_ = enc.Encode(map[string]string{
		"ts":   time.Now().UTC().Format(time.RFC3339Nano),
		"text": text,
	})
}

func setStatus(s string) {
	_ = os.WriteFile(ipc.StatusFile, []byte(s), 0644)
}
