// Package models is the menu of speech models and where they live on disk.
// Nothing ships with the build: every model is downloaded into the user's
// cache, so they are all on the same footing and none is a special case.
package models

import (
	"fmt"
	"io"
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
	// Langs are the language codes the model advertises, or nil for a model
	// that takes most of them: whisper-large-v3-turbo lists a hundred, which
	// is not a menu column. Checked against the library the same way.
	Langs []string
}

// Languages renders the language support for the menu, as the reach of the
// set and its size.
//
// Someone choosing a model wants to know whether it will handle what they
// speak, and past three or four codes a list answers that worse than a name
// for the set does: "en +29" says nothing about whether Japanese is in there,
// and neither does the eight codes that would fit the column. Naming the reach
// says which question to stop asking, and the count says how thoroughly.
func (s Spec) Languages() string {
	switch {
	case len(s.Langs) == 0:
		// A model that lists a hundred, which no column can hold and no
		// caveat improves on.
		return "Worldwide"
	case len(s.Langs) <= 2:
		// Short enough to name outright, which beats naming the reach: a
		// model taking English and Chinese is not "Worldwide (2)" to anyone
		// who speaks a third language.
		names := make([]string, len(s.Langs))
		for i, code := range s.Langs {
			names[i] = language(code)
		}
		return strings.Join(names, ", ")
	case european(s.Langs):
		return fmt.Sprintf("European (%d)", len(s.Langs))
	}
	return fmt.Sprintf("Worldwide (%d)", len(s.Langs))
}

// europeanCodes are the languages of Europe as this menu counts them. The
// borderline cases do not decide anything here: every model that advertises
// Turkish also advertises Japanese and Chinese, so it reads as worldwide
// whichever way Turkish is counted.
var europeanCodes = map[string]bool{
	"bg": true, "cs": true, "da": true, "de": true, "el": true, "en": true,
	"es": true, "et": true, "fi": true, "fr": true, "hr": true, "hu": true,
	"it": true, "lt": true, "lv": true, "mk": true, "mt": true, "nl": true,
	"pl": true, "pt": true, "ro": true, "ru": true, "sk": true, "sl": true,
	"sv": true, "uk": true,
}

func european(langs []string) bool {
	for _, code := range langs {
		if !europeanCodes[code] {
			return false
		}
	}
	return true
}

// language names a lone language, since a model that takes exactly one should
// say which rather than make its code do the work. Only the codes that appear
// alone in the menu are named; anything else falls back to the code.
func language(code string) string {
	if name, ok := map[string]string{"en": "English", "zh": "Chinese"}[code]; ok {
		return name
	}
	return code
}

// Default is what the daemon loads when not told otherwise: the model that is
// usable on the machine that has no card, since that is what a default has to
// be. Measured on a minute of dictation it makes half the errors of the
// whisper-tiny.en it replaced, 18.4% against 36.8%, and takes 223ms on a CPU
// where that whisper took 983ms and the 0.6b parakeet takes 1.1s. Anyone with
// a GPU should move up to parakeet-tdt-0.6b-v2, which the README says.
const Default = "parakeet-tdt_ctc-110m"

// Catalog is the whole menu: the models that are on the frontier of accuracy
// for their size, speed or languages, and fit in 6 GB of video memory with
// room to spare. An entry beaten on all of those at once by another entry is
// left out, since nobody should pick it.
//
// WER is the Open ASR Leaderboard's English average over its eight short-form
// sets, then its German, French and Spanish averages for the models it scores
// in those. The leaderboard stopped carrying the two smallest, so they have no
// English figure here; a number invented for them would be worse than the
// blank.
//
//	                                  en    de    fr    es
//	moonshine-tiny                                            English, the floor
//	parakeet-tdt_ctc-110m                                     English, the default
//	canary-180m-flash                 5.5                     en/de/es/fr
//	granite-speech-5.0-470m-turboctc  5.0                     English, CTC
//	parakeet-tdt-0.6b-v2              4.7                     English
//	parakeet-tdt-0.6b-v3              4.9   4.1   5.4   3.7   25 European
//	whisper-large-v3-turbo            6.4   6.1   6.7   3.9   99 languages
//	Qwen3-ASR-0.6B                    5.0   6.7   8.8   5.8   30 languages
//	canary-1b-v2                      5.7   4.1   4.8   3.2   25 European
//	Qwen3-ASR-1.7B                    4.3   4.0   5.7   3.8   30 languages
//	cohere-transcribe-03-2026         4.7   3.1   4.0   2.8   14 languages
//
// The pairs that look redundant are not. parakeet v2 is the better English
// model and v3 the one with other languages, and the leaderboard's
// close-microphone sets and the clip measured here agree on the difference:
// 3.52 against 4.07, and 12.5% against 15.4%. canary-1b-v2 takes the same 25
// languages as parakeet v3 and transcribes most of them better, at half again
// the size and a third of the speed. Qwen3-ASR-1.7B is the most accurate
// English here and cohere the most accurate everything else, and cohere does
// not take Hindi, Thai, Turkish or Russian, which Qwen3 does.
//
// Qwen3-ASR is also the one family here that decodes with a language model
// rather than an ASR head, which is the only mechanism that could get a
// technical term right from context instead of from acoustics.
//
// Two shapes of model are here, and the difference matters more than the
// sizes do. Whisper always encodes a padded 30 second window, so it costs the
// same whatever was said; the rest encode only the audio they were given.
// Measured on this laptop's CPU, the smallest whisper against
// parakeet-tdt_ctc-110m:
//
//	 2s utterance   1045ms    136ms
//	 3s utterance    960ms    235ms
//	30s utterance   2365ms   2335ms
//	55s utterance   2639ms   4768ms
//
// So the flat cost is a liability up to about 30 seconds and an asset past
// it. Dictation is mostly short utterances, which is why the menu leads with
// the models that scale with the audio, and why the small whispers are gone:
// parakeet-tdt_ctc-110m beats whisper-base.en on accuracy and on every length
// of dictation, and moonshine-tiny beats whisper-tiny.en at a smaller size.
// whisper-large-v3-turbo stays for its languages, which nothing else covers.
//
// moonshine-tiny is the floor: worth it only where nothing else fits. The
// largest entry, Qwen3-ASR-1.7B, peaked at 2.8 GiB of an RTX 4070's memory
// warming to 30 seconds and transcribing 35.
var Catalog = []Spec{
	{"moonshine-tiny", "Q8_0", 33, []string{"en"}},
	{"parakeet-tdt_ctc-110m", "Q5_K_M", 96, []string{"en"}},
	{"canary-180m-flash", "Q5_K_M", 151, []string{"en", "de", "es", "fr"}},
	{"granite-speech-5.0-470m-turboctc", "Q5_K_M", 320, []string{"en"}},
	{"parakeet-tdt-0.6b-v2", "Q5_K_M", 514, []string{"en"}},
	{"parakeet-tdt-0.6b-v3", "Q5_K_M", 523, european25},
	{"whisper-large-v3-turbo", "Q5_K_M", 590, nil},
	{"Qwen3-ASR-0.6B", "Q5_K_M", 615, qwen3Langs},
	{"canary-1b-v2", "Q5_K_M", 797, european25},
	{"Qwen3-ASR-1.7B", "Q5_K_M", 1447, qwen3Langs},
	{"cohere-transcribe-03-2026", "Q5_K_M", 1688, []string{
		"en", "ar", "de", "el", "es", "fr", "it", "ja", "ko", "nl", "pl", "pt", "vi", "zh"}},
}

// european25 is the set parakeet-tdt-0.6b-v3 and canary-1b-v2 both advertise.
var european25 = []string{
	"en", "bg", "cs", "da", "de", "el", "es", "et", "fi", "fr", "hr", "hu",
	"it", "lt", "lv", "mt", "nl", "pl", "pt", "ro", "ru", "sk", "sl", "sv", "uk"}

// qwen3Langs is the set both Qwen3-ASR sizes advertise, which is the same set.
// Nothing writes to a Spec's Langs, so entries can share one.
var qwen3Langs = []string{
	"en", "ar", "cs", "da", "de", "el", "es", "fa", "fi", "fil", "fr", "hi",
	"hu", "id", "it", "ja", "ko", "mk", "ms", "nl", "pl", "pt", "ro", "ru",
	"sv", "th", "tr", "vi", "yue", "zh"}

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
func (s Spec) File() string {
	if e, ok := elsewhere[s.Name]; ok {
		return e.file
	}
	return fmt.Sprintf("%s-%s.gguf", s.Name, s.quant)
}

// elsewhere names the models whose GGUF is published outside the
// handy-computer org, which is one repo per model named after it. These two
// were converted and published by other people before this menu wanted them,
// and fetching what exists beats asking anybody to publish a second copy of
// the same weights. The library reads both spellings of them.
var elsewhere = map[string]struct{ repo, file string }{
	"fsmn-vad":      {"FunAudioLLM/fsmn-vad-GGUF", "fsmn-vad.gguf"},
	"titanet-large": {"cstr/titanet-large-GGUF", "titanet-large.gguf"},
}

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
	// Every GGUF starts with these four bytes. Checking them turns two
	// confusing failures into one clear one: a download that was interrupted
	// between the .part rename and the disk finishing with it looks like a
	// model the menu says is present and the loader refuses, and a file that
	// was renamed to .gguf by hand looks the same. Both come back from the
	// library as "gguf load error", four words with no path in them.
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil || string(magic[:]) != "GGUF" {
		return fmt.Errorf("%s: not a GGUF file", path)
	}
	return nil
}
