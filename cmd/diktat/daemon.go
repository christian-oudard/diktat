// Daemon: keeps the model loaded, toggles recording on SIGUSR1, transcribes
// on stop, types the result.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
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
	"github.com/christian-oudard/diktat/internal/warmup"
)

const (
	statusLoad = `<span color="#fabd2f">● LOAD</span>`
	statusRec  = `<span color="#fb4934">● REC</span>`
	statusTx   = `<span color="#458588">● TX</span>`
)

// exitConfig is EX_CONFIG from sysexits.h, for a config only a person can
// fix. The unit gives it to RestartPreventExitStatus, so the daemon stops
// once and says why, rather than restarting every two seconds until the start
// limit stops it anyway and the reason is twenty entries up the journal.
const exitConfig = 78

func runDaemon(args []string) {
	if len(args) > 0 {
		log.Fatalf("daemon takes no arguments; set model in %s", config.DefaultPath())
	}

	// Install handlers before loading the model: until this runs, SIGUSR1
	// keeps its default disposition and would kill the daemon. A toggle
	// pressed during startup queues here instead.
	sigCh := make(chan os.Signal, 8)
	signal.Notify(sigCh, syscall.SIGUSR1, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)

	// A config that does not parse stops the daemon. Carrying on with an
	// empty Config was worse than it sounds: the settings that go missing are
	// the ones someone bothered to write, and nothing about dictation looks
	// broken afterwards, it just quietly does the default thing.
	cfg, unknown, err := config.Load(config.DefaultPath())
	if err != nil {
		log.Printf("config: %v", err)
		os.Exit(exitConfig)
	}
	for _, key := range unknown {
		// Silence here is how a key that had stopped meaning anything sat
		// in a real config looking like it worked.
		log.Printf("config: ignoring unknown key %q", key)
	}
	// Nothing is bundled, and nothing is downloaded implicitly: say what to
	// type instead.
	name := config.StartModel()
	modelDir := models.Resolve(name)
	if err := models.Check(modelDir); err != nil {
		log.Fatalf("%s is not downloaded. Get it with:\n  diktat model %s", name, name)
	}

	// Logging is stderr and nothing else: under systemd that is the journal,
	// which timestamps every line itself, keeps them across restarts and can
	// be followed while the daemon is running. A file of our own was one more
	// thing to find, and it was truncated at every start, so the log of the
	// crash was gone by the time anyone looked.
	log.SetFlags(0)
	log.Printf("Starting %s %s", gitRev, exePath())

	// Resolved once, here, because a daemon that cannot name these files
	// cannot do its job either, and every use below would otherwise repeat
	// the same fatal.
	pidPath, err := ipc.PIDPath()
	if err != nil {
		log.Fatal(err)
	}
	statusPath, err = ipc.StatusPath()
	if err != nil {
		log.Fatal(err)
	}
	modelPath, err = ipc.ModelPath()
	if err != nil {
		log.Fatal(err)
	}

	if err := os.WriteFile(pidPath, []byte(fmt.Sprint(os.Getpid())), 0644); err != nil {
		log.Fatalf("write pid: %v", err)
	}
	defer os.Remove(pidPath)
	defer os.Remove(statusPath)
	setStatus(statusLoad)

	// What to start on comes from config.StartModel, never from the model file
	// above: that file says what a daemon has loaded, and a daemon starting
	// has loaded nothing.
	model, err := asr.Load(modelDir)
	if err != nil {
		log.Fatalf("load model: %v", err)
	}
	defer os.Remove(modelPath)

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

	// waking is closed when the run that woke the GPU at the start of a
	// recording finishes, and woke is the model it ran on, which is not
	// necessarily the model in use by the time it lands.
	waking <-chan struct{}
	woke   *asr.Model

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
	if err := os.WriteFile(modelPath, []byte(d.modelDir), 0644); err != nil {
		log.Printf("model publish: %v", err)
	}
}

// requestModel acts on the model named in the model file. That file is a
// request on the way in and a statement of fact on the way out, so it is put
// back to the model actually loaded straight away: a switch is not a switch
// until the new model can transcribe, and a 2 GB model takes tens of seconds
// to get there.
func (d *daemon) requestModel() {
	raw, err := os.ReadFile(modelPath)
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
			// what the buckets cost; this says what came before it.
			log.Printf("Loaded %s in %s, warming", model.Name(),
				time.Since(t0).Round(time.Millisecond))
			warm(model)
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
	// Nothing may be closed while the wake run holds a model.
	d.settle()
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
// Configured in MB, or two thirds of what the compute device had free when the
// daemon started: enough to keep a couple of models around, with room left for
// the rest of the desktop on a shared laptop GPU. A device that reports no
// memory falls back to a figure that fits the models in the menu without
// assuming a big card.
//
// Free rather than total, because the third left over has to cover the compute
// buffers of whichever model is in use, and those grow with the length of the
// dictation. Struck against the total it would also be counting the memory the
// compositor already holds, which was never ours to spend.
func cacheBudget(cfg *config.Config) uint64 {
	if cfg.ModelCacheMB > 0 {
		return uint64(cfg.ModelCacheMB) << 20
	}
	if free := asr.DeviceFree(); free > 0 {
		return free / 3 * 2
	}
	return 4 << 30
}

// warm rehearses the model and says what it cost. The daemon is resident and
// loads eagerly, so the shader compiles and buffer allocations are paid here
// rather than on the first thing the user says. The cost is one throwaway
// transcription per bucket, a few hundred milliseconds in total once the
// driver has the shaders on disk, and it runs off the main loop either way.
func warm(m *asr.Model) {
	t0 := time.Now()
	work, err := warmup.Run(m)
	if err != nil {
		log.Printf("warmup: %v", err)
	}
	if work == "" {
		return
	}
	// Loading allocated the weights; those runs allocated the buffers, which
	// are the larger half and grow with the largest bucket. Now is when the
	// model's real cost is knowable, and when it can say how much longer a
	// clip it could still take.
	t := m.Timings()
	log.Printf("Warmed in %s: %s, %s resident, good for %s of audio (last bucket: mel %s, encode %s, decode %s, other %s)",
		time.Since(t0).Round(time.Millisecond), work, human.Bytes(m.Bytes()),
		m.AudioLimit().Round(time.Second),
		t.Mel.Round(time.Millisecond), t.Encode.Round(time.Millisecond),
		t.Decode.Round(time.Millisecond), t.Other.Round(time.Millisecond))
}

func (d *daemon) closeModels() {
	d.settle()
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
	d.wake()
	setStatus(statusRec)
	log.Println("Recording...")
}

// wake spends the time someone is speaking on a throwaway run, because a card
// left alone drops its clocks and the next graph pays to bring them back. That
// cost lands on the user: on granite, an utterance 25 seconds after the last
// one encoded in 993ms where the same utterance back to back encoded in 27ms.
// A single short run absorbs it, and it has seconds of speech to hide behind.
//
// The run is a length the warmup already rehearsed, so it compiles nothing,
// and it is thrown away. Errors are ignored: this is an optimisation, and the
// transcription that follows will report anything real.
func (d *daemon) wake() {
	if !d.model.OnGPU() || d.waking != nil {
		return
	}
	speech, err := warmup.Speech()
	if err != nil {
		return
	}
	done := make(chan struct{})
	d.waking, d.woke = done, d.model
	go func() {
		defer close(done)
		d.woke.Transcribe(warmup.Fit(speech, 1))
	}()
}

// settle waits for a wake run to finish. A model is single-threaded, and the
// wake holds one, so nothing else may touch a model until it is done.
func (d *daemon) settle() {
	if d.waking == nil {
		return
	}
	<-d.waking
	d.waking, d.woke = nil, nil
}

func (d *daemon) stopRecording() {
	samples := d.recorder.Stop()
	d.settle()
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
	// Only what the model would refuse, or what the card cannot hold, gets cut,
	// at the quietest moment near the limit. Most families take the whole
	// utterance and window it themselves, which they do better than a cut here
	// can: cutting at 30s cost a broken sentence at every seam even on models
	// that had no limit at all.
	limit := d.model.AudioLimit()
	chunks := audio.Chunk(samples, int(limit.Seconds())*audio.SampleRate)
	if len(chunks) > 1 {
		log.Printf("Over the %s this model can take now, transcribing in %d pieces",
			limit.Round(time.Second), len(chunks))
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
	// length will be. Expected past the last warm bucket, a bug below it, and
	// named either way, since the name says which variant the buckets missed.
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
	path := string(d.cfg.HistoryFile)
	if path[0] == '~' {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[1:])
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		log.Printf("history mkdir: %v", err)
		return
	}
	// 0600: this is every sentence ever dictated, which is the same reason
	// the last-text file is kept in a mode 0700 directory.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
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

// statusPath and modelPath are resolved at startup and used from every corner
// of the daemon, including the signal handlers, which have no daemon value to
// hang them off.
var statusPath, modelPath string

func setStatus(s string) {
	_ = os.WriteFile(statusPath, []byte(s), 0644)
}
