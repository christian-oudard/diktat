// Daemon: keeps the moonshine model loaded, toggles recording on SIGUSR1,
// transcribes on stop, types the result.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/christian-oudard/diktat/internal/asr"
	"github.com/christian-oudard/diktat/internal/audio"
	"github.com/christian-oudard/diktat/internal/config"
	"github.com/christian-oudard/diktat/internal/ipc"
	"github.com/christian-oudard/diktat/internal/output"
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

func main() {
	version := flag.Bool("version", false, "report the installed and running daemon builds")
	flag.Parse()
	if *version {
		printVersion()
		return
	}

	// Install handlers before loading the model: until this runs, SIGUSR1
	// keeps its default disposition and would kill the daemon. A toggle
	// pressed during startup queues here instead.
	sigCh := make(chan os.Signal, 8)
	signal.Notify(sigCh, syscall.SIGUSR1, syscall.SIGTERM, syscall.SIGINT)

	if logf, err := os.OpenFile(ipc.LogFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err == nil {
		log.SetOutput(logf)
	}
	log.SetFlags(log.Ltime)
	log.Printf("Starting %s", exePath())

	if err := os.WriteFile(ipc.PIDFile, []byte(fmt.Sprint(os.Getpid())), 0644); err != nil {
		log.Fatalf("write pid: %v", err)
	}
	defer os.Remove(ipc.PIDFile)
	defer os.Remove(ipc.StatusFile)
	setStatus(statusLoad)

	modelDir := os.Getenv("MOONSHINE_MODEL_DIR")
	ortLib := os.Getenv("ONNXRUNTIME_LIB")
	if modelDir == "" || ortLib == "" {
		log.Fatal("MOONSHINE_MODEL_DIR and ONNXRUNTIME_LIB must be set")
	}

	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		log.Printf("config: %v (continuing with defaults)", err)
		cfg = &config.Config{}
	}

	model, err := asr.LoadModel(modelDir, ortLib)
	if err != nil {
		log.Fatalf("load model: %v", err)
	}
	defer model.Close()

	rec, err := audio.NewRecorder()
	if err != nil {
		log.Fatalf("audio recorder: %v", err)
	}
	defer rec.Close()
	log.Println("Model loaded, idle.")
	setStatus("")

	capCh := make(chan struct{}, 1)
	d := &daemon{
		model:    model,
		recorder: rec,
		cfg:      cfg,
	}
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
	model     *asr.Model
	recorder  *audio.Recorder
	cfg       *config.Config
	capTimer  *time.Timer
	startedAt time.Time

	mu        sync.Mutex
	recording bool
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
	if err := audio.WriteWAV(ipc.LastAudioFile, samples, audio.SampleRate); err != nil {
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
	log.Printf("Transcribed in %s: %q", time.Since(t0), text)

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

// exePath is the store path of this build, which is what distinguishes one
// build of diktat from another.
func exePath() string {
	path, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	return path
}

func printVersion() {
	installed := exePath()
	fmt.Println("installed:", installed)
	pid := ipc.ReadPID()
	if pid == 0 {
		fmt.Println("running:   no daemon")
		return
	}
	running := ipc.ExePath(pid)
	fmt.Printf("running:   %s (pid %d)\n", running, pid)
	if running != installed {
		fmt.Println("The running daemon is stale. Restart it with: systemctl --user restart diktat")
	}
}
