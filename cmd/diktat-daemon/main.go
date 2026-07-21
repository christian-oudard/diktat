// Daemon: keeps the moonshine model loaded, toggles recording on SIGUSR1,
// transcribes on stop, types the result.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/christian-oudard/diktat/internal/asr"
	"github.com/christian-oudard/diktat/internal/audio"
	"github.com/christian-oudard/diktat/internal/config"
	"github.com/christian-oudard/diktat/internal/output"
)

const (
	pidFile      = "/tmp/diktat-daemon.pid"
	statusFile   = "/tmp/diktat-status"
	lastTextFile = "/tmp/diktat-last"
	logFile      = "/tmp/diktat-daemon.log"

	statusLoad = `<span color="#fabd2f">● LOAD</span>`
	statusRec  = `<span color="#fb4934">● REC</span>`
	statusTx   = `<span color="#458588">● TX</span>`

	idleTimeout = 15 * time.Minute
)

func main() {
	flag.Parse()

	if logf, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err == nil {
		log.SetOutput(logf)
	}
	log.SetFlags(log.Ltime)

	if err := os.WriteFile(pidFile, []byte(fmt.Sprint(os.Getpid())), 0644); err != nil {
		log.Fatalf("write pid: %v", err)
	}
	defer os.Remove(pidFile)
	defer os.Remove(statusFile)
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
	log.Println("Model loaded.")

	d := &daemon{
		model:    model,
		recorder: rec,
		cfg:      cfg,
	}

	sigCh := make(chan os.Signal, 8)
	signal.Notify(sigCh, syscall.SIGUSR1, syscall.SIGTERM, syscall.SIGINT)

	d.idleTimer = time.AfterFunc(idleTimeout, func() {
		d.mu.Lock()
		recording := d.recording
		d.mu.Unlock()
		if recording {
			return
		}
		log.Println("Idle timeout, shutting down.")
		sigCh <- syscall.SIGTERM
	})
	d.idleTimer.Stop()

	d.startRecording()

	for sig := range sigCh {
		switch sig {
		case syscall.SIGUSR1:
			d.mu.Lock()
			recording := d.recording
			d.mu.Unlock()
			if recording {
				d.stopRecording()
			} else {
				d.startRecording()
			}
		case syscall.SIGTERM, syscall.SIGINT:
			d.mu.Lock()
			if d.recording {
				d.recording = false
			}
			d.mu.Unlock()
			log.Println("Daemon stopped.")
			return
		}
	}
}

type daemon struct {
	model     *asr.Model
	recorder  *audio.Recorder
	cfg       *config.Config
	idleTimer *time.Timer

	mu        sync.Mutex
	recording bool
}

func (d *daemon) startRecording() {
	d.idleTimer.Stop()
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
		setStatus("")
		d.idleTimer.Reset(idleTimeout)
		log.Println("No audio.")
		return
	}

	setStatus(statusTx)
	peak, rms := levels(samples)
	log.Printf("Transcribing %.1fs (peak %.3f rms %.4f)...",
		float64(len(samples))/float64(audio.SampleRate), peak, rms)

	t0 := time.Now()
	text, err := d.model.Transcribe(samples)
	if err != nil {
		log.Printf("transcribe: %v", err)
		setStatus("")
		d.idleTimer.Reset(idleTimeout)
		return
	}
	log.Printf("Transcribed in %s: %q", time.Since(t0), text)

	if text != "" {
		out := text + " "
		if err := os.WriteFile(lastTextFile, []byte(out), 0644); err != nil {
			log.Printf("last-text write: %v", err)
		}
		d.appendHistory(text)
		if err := output.Type(out, d.cfg.PasteMethods); err != nil {
			log.Printf("type: %v", err)
		}
	}

	setStatus("")
	d.idleTimer.Reset(idleTimeout)
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

// levels returns the peak absolute amplitude and RMS of the samples, to
// distinguish silent/low-gain captures from genuine speech in the log.
func levels(samples []float32) (peak, rms float64) {
	var sumSq float64
	for _, s := range samples {
		a := math.Abs(float64(s))
		if a > peak {
			peak = a
		}
		sumSq += float64(s) * float64(s)
	}
	if len(samples) > 0 {
		rms = math.Sqrt(sumSq / float64(len(samples)))
	}
	return peak, rms
}

func setStatus(s string) {
	_ = os.WriteFile(statusFile, []byte(s), 0644)
}
