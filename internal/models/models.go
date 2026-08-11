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

type Kind int

const (
	Moonshine Kind = iota
	Whisper
)

// Spec is one entry in the menu.
type Spec struct {
	Name string
	Kind Kind
	// size is the upstream's name for it, which is not always our name.
	size string
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
//	large-v3-turbo-q5_0     3.8% WER  146ms
//
// medium.en and distil-large-v3.5 are left out as dominated: both are larger
// and slower than large-v3-turbo and score worse. turbo is large-v3 with the
// decoder distilled from 32 layers to 4, which is why it costs no more than
// medium despite being a bigger model.
var Catalog = []Spec{
	{"moonshine-tiny", Moonshine, "tiny", 106},
	{"moonshine-base", Moonshine, "base", 238},
	{"whisper-tiny.en", Whisper, "tiny.en", 75},
	{"whisper-base.en", Whisper, "base.en", 142},
	{"whisper-small.en", Whisper, "small.en", 487},
	{"whisper-large-v3-turbo-q5_0", Whisper, "large-v3-turbo-q5_0", 574},
	{"whisper-large-v3-turbo", Whisper, "large-v3-turbo", 1624},
}

// Dir is where downloaded models live.
func Dir() string {
	if cache := os.Getenv("XDG_CACHE_HOME"); cache != "" {
		return filepath.Join(cache, "diktat", "models")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "diktat", "models")
}

// Path is where a menu entry lands once downloaded. Moonshine is a directory
// of ONNX files, whisper a single ggml file, which is how the daemon tells
// them apart.
func (s Spec) Path() string {
	if s.Kind == Whisper {
		return filepath.Join(Dir(), s.Name+".bin")
	}
	return filepath.Join(Dir(), s.Name)
}

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

// Check reports whether path holds a model the daemon can load.
func Check(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if !strings.HasSuffix(path, ".bin") {
			return fmt.Errorf("%s: not a whisper .bin", path)
		}
		return nil
	}
	for _, f := range []string{"encoder.onnx", "decoder.onnx", "tokenizer.json"} {
		if _, err := os.Stat(filepath.Join(path, f)); err != nil {
			return fmt.Errorf("%s: incomplete moonshine model, missing %s", path, f)
		}
	}
	return nil
}
