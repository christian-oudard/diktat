// Package models is the menu of speech models and where they live on disk.
// Nothing ships with the build: every model is downloaded into the user's
// cache, so they are all on the same footing and none is a special case.
package models

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/christian-oudard/diktat/internal/human"
)

// Spec is one entry in the menu.
type Spec struct {
	Name string
	// quant is the quantization published for this model. Whisper ships a
	// K-quant, moonshine only Q8_0.
	quant string
	// MiB is the download size in mebibytes, so the menu can show the cost
	// of fetching one. Measured from the published file, not converted from a
	// decimal figure: the two differ by 5% and the column says MiB.
	MiB int
	// Vocab says the family takes vocabulary hints, which are whisper's
	// initial prompt. Architectural rather than per-file, so it is recorded
	// here instead of loading every model to ask: the menu has to answer
	// before anything is downloaded. TestVocabMatchesLibrary checks it
	// against the library for whatever is present.
	Vocab bool
	// Langs are the language codes the model advertises, or nil for a model
	// that takes most of them: whisper-large-v3-turbo lists a hundred, which
	// is not a menu column. Checked against the library the same way.
	Langs []string
}

// listedLangs is how many codes are worth spelling out before a count says
// more than the list does. Five fits the column; eight does not.
const listedLangs = 5

// Languages renders the language support for the menu.
func (s Spec) Languages() string {
	switch {
	case len(s.Langs) == 0:
		return "all"
	case len(s.Langs) <= listedLangs:
		return strings.Join(s.Langs, " ")
	}
	// Past that, the first code and a count: which eight is not something a
	// menu column can usefully say, and the model's own card can.
	return fmt.Sprintf("%s +%d", s.Langs[0], len(s.Langs)-1)
}

// Default is what the daemon loads when not told otherwise. Still whisper,
// which the measurements below say is the wrong choice for short utterances,
// because the default is what gets dictated through every day and the
// alternatives have not been used in anger yet. Switch it once they have.
const Default = "whisper-tiny.en"

// Catalog is the whole menu: one entry per niche, not everything upstream
// publishes. WER is the Open ASR Leaderboard average over its eight
// short-form English sets, which is a better guide for dictation than
// LibriSpeech alone.
//
//	whisper-tiny.en                       English, the default
//	parakeet-tdt_ctc-110m      6.6% WER   English
//	parakeet-tdt-0.6b-v2       5.4% WER   English
//	whisper-large-v3-turbo     7.0% WER   99 languages
//	canary-1b-flash            5.8% WER   en/de/es/fr, and translation
//	granite-speech-4.1-2b-nar  4.9% WER   English, no timestamps
//	Voxtral-Mini-3B-2507       6.0% WER   8 languages, and translation
//
// whisper-tiny.en is not on that leaderboard and would place last of these
// if it were; it stays the default only until the newer models have been
// lived with, since the default is what gets dictated through every day.
//
// Two shapes of model are here, and the difference matters more than the
// sizes do. Whisper always encodes a padded 30 second window, so it costs
// the same whatever was said; the rest encode only the audio they were
// given. Measured on this laptop's CPU, whisper-tiny.en against
// parakeet-tdt_ctc-110m:
//
//	 2s utterance   1045ms    136ms
//	 3s utterance    960ms    235ms
//	30s utterance   2365ms   2335ms
//	55s utterance   2639ms   4768ms
//
// So the flat cost is a liability up to about 30 seconds and an asset past
// it. Dictation is mostly short utterances, which is why the menu leads with
// the models that scale with the audio; the 60 second recording cap is the
// only place the crossover is reachable. One whisper stays for the languages
// the others lack.
//
// Whisper and voxtral take vocabulary hints, by different mechanisms, and
// nothing else here does. Whisper conditions its decoder on the words, which
// is mechanical; voxtral is an audio-LLM and gets them as an instruction it
// may follow loosely. That whisper can be biased at all is most of its
// remaining argument in this menu.
//
// moonshine-tiny is the floor: worth it only where nothing else fits.
// granite is the ceiling on accuracy, and voxtral the ceiling on size. Both
// are big enough that the cache budget will evict something to hold them, and
// voxtral is here for its languages and its hints rather than its numbers:
// parakeet-tdt-0.6b-v2 beats it on accuracy at a fifth of the size.
var Catalog = []Spec{
	{"moonshine-tiny", "Q8_0", 33, false, []string{"en"}},
	{"whisper-tiny.en", "Q5_K_M", 42, true, []string{"en"}},
	{"parakeet-tdt_ctc-110m", "Q5_K_M", 96, false, []string{"en"}},
	{"parakeet-tdt-0.6b-v2", "Q5_K_M", 514, false, []string{"en"}},
	{"whisper-large-v3-turbo", "Q5_K_M", 590, true, nil},
	{"canary-1b-flash", "Q5_K_M", 733, false, []string{"en", "de", "es", "fr"}},
	{"granite-speech-4.1-2b-nar", "Q5_K_M", 1699, false, []string{"en", "de", "es", "fr", "pt"}},
	{"Voxtral-Mini-3B-2507", "Q4_K_M", 2846, true, []string{"en", "fr", "de", "es", "it", "pt", "nl", "hi"}},
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

// Size is the download, for the menu and for the prompt before fetching one.
func (s Spec) Size() string { return human.Bytes(uint64(s.MiB) << 20) }

// Path is where a menu entry lands once downloaded.
func (s Spec) Path() string { return filepath.Join(Dir(), s.File()) }

// Downloaded reports whether the model is present and complete.
func (s Spec) Downloaded() bool {
	return Check(s.Path()) == nil
}

// Lookup finds a menu entry by name, or by its position in the menu counting
// from 1. The names run to twenty-odd characters and the menu is short, so
// the number is what anyone switching models by hand will reach for. Names
// are matched first, so a model named for a number would still win.
func Lookup(nameOrNumber string) (Spec, bool) {
	for _, s := range Catalog {
		if s.Name == nameOrNumber {
			return s, true
		}
	}
	if n, err := strconv.Atoi(nameOrNumber); err == nil && n >= 1 && n <= len(Catalog) {
		return Catalog[n-1], true
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
