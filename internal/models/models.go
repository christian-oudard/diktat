// Package models is the menu of speech models and where they live on disk.
// Nothing ships with the build: every model is downloaded into the user's
// cache, so they are all on the same footing and none is a special case.
package models

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Spec is one entry in the menu.
type Spec struct {
	Name string
	// quant is the quantization published for this model. Whisper ships a
	// K-quant, moonshine only Q8_0.
	quant string
	// MB is the download size, so the menu can show the cost of fetching one.
	MB int
}

// Default is what the daemon loads when not told otherwise.
const Default = "whisper-tiny.en"

// Catalog is the whole menu. Kept short on purpose: these are the ones worth
// choosing between, not everything upstream publishes. Word error rate is on
// 300 utterances of LibriSpeech test-other, the noisy split, measured on a
// discrete GPU; latency is per utterance and flat, since whisper encodes a
// padded 30 second window whatever was said.
//
//	tiny.en                13.4% WER   16ms
//	base.en                10.3% WER   22ms
//	small.en                6.7% WER   52ms
//	large-v3-turbo          4.1% WER  139ms
//
// medium.en and distil-large-v3.5 are left out as dominated: both are larger
// and slower than large-v3-turbo and score worse. turbo is large-v3 with the
// decoder distilled from 32 layers to 4, which is why it costs no more than
// medium despite being a bigger model.
//
// Moonshine is here for its shape rather than its accuracy: it encodes only
// the audio it was given instead of padding to 30 seconds, so on short
// utterances it is far cheaper than its size suggests.
var Catalog = []Spec{
	{"moonshine-tiny", "Q8_0", 35},
	{"moonshine-base", "Q8_0", 77},
	{"whisper-tiny.en", "Q5_K_M", 44},
	{"whisper-base.en", "Q5_K_M", 63},
	{"whisper-small.en", "Q5_K_M", 193},
	{"whisper-large-v3-turbo", "Q5_K_M", 619},
}

// Dir is where downloaded models live.
func Dir() string {
	if cache := os.Getenv("XDG_CACHE_HOME"); cache != "" {
		return filepath.Join(cache, "diktat", "models")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "diktat", "models")
}

// File is the GGUF's name, which carries the quantization so two quants of
// one model can sit side by side in the cache.
func (s Spec) File() string { return fmt.Sprintf("%s-%s.gguf", s.Name, s.quant) }

// Path is where a menu entry lands once downloaded.
func (s Spec) Path() string { return filepath.Join(Dir(), s.File()) }

// Downloaded reports whether the model is present and complete.
func (s Spec) Downloaded() bool {
	return Check(s.Path()) == nil
}

// Lookup finds a menu entry by name.
func Lookup(name string) (Spec, bool) {
	for _, s := range Catalog {
		if s.Name == name {
			return s, true
		}
	}
	return Spec{}, false
}

// Names lists the menu.
func Names() []string {
	out := make([]string, 0, len(Catalog))
	for _, s := range Catalog {
		out = append(out, s.Name)
	}
	return out
}

// Resolve turns a menu name into a path. Anything containing a separator is
// taken as a path and used as given, so an out-of-menu model still works.
func Resolve(nameOrPath string) string {
	if strings.ContainsRune(nameOrPath, filepath.Separator) || nameOrPath == "." {
		if abs, err := filepath.Abs(nameOrPath); err == nil {
			return abs
		}
		return nameOrPath
	}
	if s, ok := Lookup(nameOrPath); ok {
		return s.Path()
	}
	return filepath.Join(Dir(), nameOrPath)
}

// Check reports whether path holds a model the daemon can load. Whether the
// GGUF is one of the architectures the library implements is its business,
// not ours; this only rules out the obvious mistakes.
func Check(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s: a directory, not a .gguf", path)
	}
	if !strings.HasSuffix(path, ".gguf") {
		return fmt.Errorf("%s: not a .gguf", path)
	}
	return nil
}
