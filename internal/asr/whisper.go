package asr

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/christian-oudard/diktat/internal/wav"
)

// Whisper runs whisper.cpp over its CLI. There is no cgo binding here on
// purpose: this exists to judge whisper against moonshine on real dictation,
// and shelling out is enough to answer that. It reloads the model per
// utterance, so it is slower than moonshine by a fixed cost.
type Whisper struct {
	modelPath string
}

// LoadWhisper checks the ggml model and the CLI are both present, so a bad
// switch fails at load rather than on the first utterance.
func LoadWhisper(modelPath string) (*Whisper, error) {
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("whisper model: %w", err)
	}
	if _, err := exec.LookPath("whisper-cli"); err != nil {
		return nil, fmt.Errorf("whisper-cli not on PATH: %w", err)
	}
	return &Whisper{modelPath: modelPath}, nil
}

func (w *Whisper) Arch() string {
	return "whisper.cpp " + strings.TrimSuffix(filepath.Base(w.modelPath), ".bin")
}

func (w *Whisper) Close() {}

// Transcribe writes the samples to a temporary 16 kHz WAV, since whisper-cli
// reads a file rather than stdin.
func (w *Whisper) Transcribe(audio []float32) (string, error) {
	dir, err := os.MkdirTemp("", "diktat-whisper")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "audio.wav")
	if err := wav.WriteWAV(path, audio, wav.SampleRate); err != nil {
		return "", err
	}

	out, err := exec.Command("whisper-cli",
		"-m", w.modelPath, "-f", path, "-nt", "-np", "-l", "en").Output()
	if err != nil {
		return "", fmt.Errorf("whisper-cli: %w", err)
	}
	// -nt drops timestamps, -np drops the banner, so what is left is the text,
	// which whisper pads with spaces and splits over segment lines.
	return strings.TrimSpace(strings.Join(strings.Fields(string(out)), " ")), nil
}
