// Package asr transcribes audio with transcribe.cpp, which covers whisper,
// moonshine and the other families behind one API. The model is a GGUF file;
// which architecture it is comes from the file, not from us.
package asr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	transcribe "github.com/handy-computer/transcribe.cpp/bindings/go"
)

// quiet drops the library's own narration. It logs model loading to stderr
// otherwise, and the daemon has its own log while the offline tools print
// results. Once per process, since the sink is global.
var quiet sync.Once

// Model is a loaded recognizer. It holds the weights and the decoder state,
// so it is worth keeping resident, and it is single-threaded.
type Model struct {
	s    *transcribe.Session
	name string
	// gpu is the device it landed on, or "" for CPU.
	gpu string
}

// Load opens a GGUF model and keeps it open.
func Load(path string) (*Model, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}
	quiet.Do(func() { transcribe.SetLogHandler(nil) })

	opts, gpu, err := placement()
	if err != nil {
		return nil, err
	}
	s, err := transcribe.Open(path, opts, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return &Model{
		s:    s,
		name: strings.TrimSuffix(filepath.Base(path), ".gguf"),
		gpu:  gpu,
	}, nil
}

// placement decides where compute runs, and names the device it chose.
//
// The library takes the first GPU it finds, integrated or not, so on a hybrid
// laptop it can land on the Intel chip rather than the discrete card. An iGPU
// shares memory bandwidth with the CPU it would be replacing and is no clear
// win, so pick the discrete one explicitly and stay on the CPU when there is
// none. DIKTAT_GPU overrides: 0 forces CPU, 1 takes whatever the library
// would have picked unaided, including an integrated GPU.
func placement() (*transcribe.LoadOptions, string, error) {
	if v, err := strconv.ParseBool(os.Getenv("DIKTAT_GPU")); err == nil {
		if !v {
			return &transcribe.LoadOptions{Backend: transcribe.BackendCPU}, "", nil
		}
		return nil, "auto", nil
	}

	devices, err := transcribe.Devices()
	if err != nil {
		return nil, "", err
	}
	for i, d := range devices {
		if d.Type != transcribe.DeviceGPU {
			continue
		}
		// Index 0 is not selectable explicitly, since zero means auto, but
		// a device that is first in probe order is what auto picks anyway.
		if i == 0 {
			return nil, d.Name, nil
		}
		return &transcribe.LoadOptions{GPUDevice: i}, d.Name, nil
	}
	return &transcribe.LoadOptions{Backend: transcribe.BackendCPU}, "", nil
}

// Arch names the model and the device, because the difference between GPU and
// CPU here is two orders of magnitude on the encoder and falling back is
// silent. It lists everything the library found, so picking the wrong chip on
// a hybrid laptop shows up rather than hiding behind a plain "gpu".
func (m *Model) Arch() string {
	where := "cpu"
	if m.gpu != "" {
		where = "gpu " + m.gpu
	}
	return fmt.Sprintf("%s, %s [%s]", m.name, where, Devices())
}

// Devices describes every compute device the library registered.
func Devices() string {
	quiet.Do(func() { transcribe.SetLogHandler(nil) })
	devices, err := transcribe.Devices()
	if err != nil {
		return "unknown"
	}
	out := make([]string, 0, len(devices))
	for _, d := range devices {
		out = append(out, fmt.Sprintf("%s (%s)", d.Description, d.Type))
	}
	return strings.Join(out, ", ")
}

func (m *Model) Close() {
	if m.s != nil {
		m.s.Close()
		m.s = nil
	}
}

// Transcribe runs the model over 16 kHz mono samples. Whisper pads every
// utterance to a 30 second window internally, so its cost is roughly flat
// however long the utterance was; moonshine encodes only what it was given.
func (m *Model) Transcribe(audio []float32) (string, error) {
	if len(audio) == 0 {
		return "", nil
	}
	res, err := m.s.Run(context.Background(), audio, nil)
	if err != nil {
		return "", err
	}
	return dropAnnotations(res.Text), nil
}

// dropAnnotations removes non-speech markers. Whisper emits things like
// [BLANK_AUDIO], (wind blowing) or a bare musical note for audio it hears no
// words in. Moonshine returns an empty string for the same input, and the
// daemon only skips typing on empty, so without this a silent capture types
// "[BLANK_AUDIO]" into whatever has focus.
func dropAnnotations(text string) string {
	out := strings.Join(strings.Fields(text), " ")
	for {
		trimmed := annotation.ReplaceAllString(out, "")
		trimmed = strings.TrimSpace(strings.Join(strings.Fields(trimmed), " "))
		if trimmed == out {
			break
		}
		out = trimmed
	}
	return out
}

// Bracketed or parenthesised asides, and the note glyphs whisper uses for
// music. Deliberately not anchored: an annotation can sit beside real speech.
var annotation = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)|[♪♫]`)
