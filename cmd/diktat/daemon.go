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

// A recording nobody stops would grow the sample buffer forever, and the
// encoder's cost grows with utterance length, so cut one off and transcribe
// what we have. audio.MaxRecording is already past what the decoder can emit:
// it stops after maxLen tokens, roughly a minute of speech.
const maxRecording = audio.MaxRecording

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
	// Nothing is bundled, so the daemon starts on the last model chosen, then
	// the configured one, then the default. The choice outranks the config
	// because it is the more recent instruction from the same person, and it
	// is cleared by deleting one file. Never download implicitly; say what to
	// type.
	name := config.Selected()
	if name == "" {
		name = cfg.Model
	}
	if name == "" {
		name = models.Default
	}
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

	capCh := make(chan struct{}, 1)
	d := &daemon{
		model:    model,
		models:   map[string]*asr.Model{modelDir: model},
		lru:      []string{modelDir},
		budget:   cacheBudget(cfg),
		recorder: rec,
		cfg:      cfg,
		modelDir: modelDir,
	}
	defer d.closeModels()
	d.applyVocabulary(model)
	warm(model)
	d.publishModel()
	log.Printf("Model loaded, idle: %s (%s)", modelDir, model.Arch())
	setStatus("")
	d.capTimer = time.AfterFunc(maxRecording, func() {
		select {
		case capCh <- struct{}{}:
		default:
		}
	})
	d.capTimer.Stop()

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
				d.reloadModel()
			case syscall.SIGTERM, syscall.SIGINT:
				d.capTimer.Stop()
				d.mu.Lock()
				d.recording = false
				d.mu.Unlock()
				log.Println("Daemon stopped.")
				return
			}
		case <-capCh:
			// The timer is stopped whenever recording stops, so this normally
			// only fires mid-recording. Guard anyway against a stale tick that
			// was already queued.
			if d.isRecording() {
				log.Printf("Hit the %s recording cap.", maxRecording)
				d.stopRecording()
			}
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
	capTimer  *time.Timer
	startedAt time.Time
	modelDir  string

	mu        sync.Mutex
	recording bool
}

func (d *daemon) publishModel() {
	if err := os.WriteFile(ipc.ModelFile, []byte(d.modelDir), 0644); err != nil {
		log.Printf("model publish: %v", err)
	}
}

// reloadModel swaps in the model named in ipc.ModelFile. Models stay resident
// once loaded, so switching back and forth costs nothing after the first load.
// Recording is not interrupted: the capture buffer is independent of the model,
// so a swap while armed just means the new model transcribes what was captured.
func (d *daemon) reloadModel() {
	raw, err := os.ReadFile(ipc.ModelFile)
	if err != nil {
		log.Printf("model reload: %v", err)
		return
	}
	dir := strings.TrimSpace(string(raw))
	if dir == d.modelDir {
		log.Printf("Already using %s", dir)
		return
	}

	if model, ok := d.models[dir]; ok {
		d.model, d.modelDir = model, dir
		d.touch(dir)
		log.Printf("Model now %s (%s), already resident", dir, model.Arch())
		d.restoreStatus()
		return
	}

	setStatus(statusLoad)
	t0 := time.Now()
	model, err := asr.Load(dir)
	if err != nil {
		// Keep serving with the model we have, and put the file back so it
		// keeps describing what is actually loaded.
		log.Printf("model reload %s: %v", dir, err)
		d.publishModel()
		d.restoreStatus()
		return
	}
	d.applyVocabulary(model)
	warm(model)
	d.models[dir] = model
	d.model, d.modelDir = model, dir
	d.touch(dir)
	d.evict()
	log.Printf("Model now %s (%s) in %s, %d resident, %d MB cached",
		dir, model.Arch(), time.Since(t0).Round(time.Millisecond), len(d.models), d.cached()>>20)
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
		log.Printf("Vocabulary hints ignored: %s takes no initial prompt", m.Arch())
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
		log.Printf("Evicting %s (%d MB), cache over %d MB budget",
			dir, sizes[dir]>>20, d.budget>>20)
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

// warmSeconds is how much audio the warmup pretends to have heard. It wants
// to be near a real utterance: the models that encode only what they were
// given build a graph sized to the input, so warming on one second leaves
// the shapes a real utterance needs still to be compiled, which was the
// whole point of warming.
const warmSeconds = 4

// warm runs a throwaway transcription, because loading a model is not the
// same as being ready to use it: the Vulkan backend defers compiling its
// shaders, and ggml defers allocating its compute buffers, to the first
// graph run. The daemon is resident and loads eagerly, so pay that here
// rather than on the first thing the user says.
//
// The input is faint noise rather than digital silence, which costs the same
// (measured: parakeet and whisper both decode silence at full price, so
// neither skips it) and cannot be skipped by a family that does look for
// speech before decoding.
func warm(m *asr.Model) {
	t0 := time.Now()
	if _, err := m.Transcribe(warmupAudio()); err != nil {
		log.Printf("warmup: %v", err)
		return
	}
	// Loading allocated the weights; that run allocated the buffers, which
	// are the larger half. Now is when the model's real cost is knowable.
	m.Measure()
	t := m.Timings()
	log.Printf("Warmed up in %s (encode %s, decode %s), %d MB resident",
		time.Since(t0).Round(time.Millisecond),
		t.Encode.Round(time.Millisecond), t.Decode.Round(time.Millisecond),
		m.Bytes()>>20)
}

// warmupAudio is quiet broadband noise, loud enough to be audio rather than
// nothing and far too quiet to be words.
func warmupAudio() []float32 {
	buf := make([]float32, warmSeconds*audio.SampleRate)
	// A cheap deterministic wobble; nothing here needs randomness, and a
	// fixed pattern keeps one load comparable with the next.
	for i := range buf {
		buf[i] = float32((i%17)-8) / 8000
	}
	return buf
}

func (d *daemon) closeModels() {
	for _, m := range d.models {
		m.Close()
	}
}

func (d *daemon) restoreStatus() {
	if d.isRecording() {
		setStatus(statusRec)
		return
	}
	setStatus("")
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
	d.capTimer.Reset(maxRecording)
	setStatus(statusRec)
	log.Println("Recording...")
}

func (d *daemon) stopRecording() {
	d.capTimer.Stop()
	samples := d.recorder.Stop()
	d.mu.Lock()
	d.recording = false
	d.mu.Unlock()

	if len(samples) == 0 {
		setStatus("")
		log.Println("No audio.")
		return
	}

	setStatus(statusTx)
	// Before Normalize, which rewrites samples in place.
	if err := wav.WriteWAV(ipc.LastAudioFile, samples, audio.SampleRate); err != nil {
		log.Printf("last-audio write: %v", err)
	}
	peak, rms := audio.Levels(samples)
	gain := audio.Normalize(samples)
	// Audio duration is derived from the sample count at the rate we asked the
	// device for. If it drifts from the wall clock, the device is not actually
	// giving us that rate, and the model is seeing time-stretched speech.
	log.Printf("Transcribing %.1fs (wall %.1fs, peak %.3f rms %.4f gain %.1fx)...",
		float64(len(samples))/float64(audio.SampleRate), time.Since(d.startedAt).Seconds(),
		peak, rms, gain)

	t0 := time.Now()
	text, err := d.model.Transcribe(samples)
	if err != nil {
		log.Printf("transcribe: %v", err)
		setStatus("")
		return
	}
	// The text itself is deliberately not logged: the log is a long-lived file
	// in /tmp and everything dictated would accumulate in it. Length is enough
	// to tell "heard nothing" from "heard something" when reading the log.
	// The breakdown separates the model's own work from everything around
	// it, which is what tells a slow model from a cold one: a first
	// utterance that spends its time in encode is still compiling shaders.
	tm := d.model.Timings()
	log.Printf("Transcribed in %s (encode %s, decode %s): %d chars",
		time.Since(t0).Round(time.Millisecond),
		tm.Encode.Round(time.Millisecond), tm.Decode.Round(time.Millisecond), len(text))

	if text != "" {
		out := text + " "
		if err := os.WriteFile(ipc.LastTextFile, []byte(out), 0644); err != nil {
			log.Printf("last-text write: %v", err)
		}
		d.appendHistory(text)
		if err := output.Type(out, d.cfg.PasteMethods); err != nil {
			log.Printf("type: %v", err)
		}
	}

	setStatus("")
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
