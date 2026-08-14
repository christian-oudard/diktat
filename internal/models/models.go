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
	// Vocab says the family can be conditioned on a vocabulary list, by a
	// prompt or by an instruction. Architectural rather than per-file, so it is recorded
	// here instead of loading every model to ask: the menu has to answer
	// before anything is downloaded. TestVocabMatchesLibrary checks it
	// against the library for whatever is present.
	Vocab bool
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
	case len(s.Langs) == 1:
		return language(s.Langs[0])
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
	if name, ok := map[string]string{"en": "English"}[code]; ok {
		return name
	}
	return code
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
//	whisper-base.en                       English, a step above the default
//	parakeet-tdt_ctc-110m      6.6% WER   English
//	canary-180m-flash                     4 languages at a tenth of the 1b
//	whisper-small.en                      English, hints without the size
//	parakeet-tdt-0.6b-v2       5.4% WER   English
//	parakeet-tdt-0.6b-v3                  the v2 with 25 European languages
//	whisper-large-v3-turbo     7.0% WER   99 languages
//	Qwen3-ASR-0.6B                        30 languages, the widest small one
//	canary-1b-flash            5.8% WER   en/de/es/fr, and translation
//	Qwen3-ASR-1.7B                        the 0.6B's next size up
//	cohere-transcribe-03-2026             14 languages
//	granite-speech-4.1-2b-nar  4.9% WER   English, no timestamps
//	canary-qwen-2.5b                      English, an LLM for a decoder
//
// The blanks are models the leaderboard does not carry: the small whispers
// because it only measures large-v3-turbo, and the recent ones because it has
// not caught up with them. A number invented here would be worse than the
// blank. Those are in the menu to be tried, not because they are known good.
//
// canary-qwen-2.5b is the one architecture here that decodes with a language
// model rather than an ASR head, which is the only mechanism that could get a
// technical term right from context instead of from acoustics. It takes no
// instruction, so unlike Voxtral it cannot be talked into answering with
// something other than a transcript.
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
// the models that scale with the audio, and the crossover is only reachable
// by someone talking for half a minute without stopping. One whisper stays
// for the languages the others lack.
//
// Only whisper takes vocabulary hints, which is most of its remaining
// argument here: it is the one family the library can condition at all. So
// the hinted models are a size progression of their own, and it used to run
// from tiny.en straight to large-v3-turbo: fourteen times the size and
// ninety-eight unused languages, to get better English. base.en and small.en
// are the sizes between.
//
// Voxtral could take them too, as an instruction rather than a prompt, and
// was in this menu for it. It left because whisper-large-v3-turbo does the
// same job better on every axis that matters: a fifth the size, ninety-nine
// languages against eight, and a decoder that is biased rather than an
// audio-LLM that is asked. Voxtral won on a point of WER and lost on
// everything else. asr.promptOptions still knows how to instruct one, so a
// path to a voxtral GGUF outside this menu keeps working.
//
// moonshine-tiny is the floor: worth it only where nothing else fits.
// granite is the ceiling on accuracy, and cohere-transcribe on size; both are
// big enough that the cache budget will evict something to hold them.
//
// parakeet-tdt-0.6b-v3 is here beside v2 rather than instead of it. On paper
// it dominates: nine more mebibytes for twenty-four more languages, same
// family and same shape. Whether it gives up English accuracy for them is not
// something the leaderboard answers yet, so both stay until one has been
// dictated through.
var Catalog = []Spec{
	{"moonshine-tiny", "Q8_0", 33, false, []string{"en"}},
	{"whisper-tiny.en", "Q5_K_M", 42, true, []string{"en"}},
	{"whisper-base.en", "Q5_K_M", 60, true, []string{"en"}},
	{"parakeet-tdt_ctc-110m", "Q5_K_M", 96, false, []string{"en"}},
	{"canary-180m-flash", "Q5_K_M", 151, false, []string{"en", "de", "es", "fr"}},
	{"whisper-small.en", "Q5_K_M", 184, true, []string{"en"}},
	{"parakeet-tdt-0.6b-v2", "Q5_K_M", 514, false, []string{"en"}},
	{"parakeet-tdt-0.6b-v3", "Q5_K_M", 523, false, []string{
		"en", "bg", "cs", "da", "de", "el", "es", "et", "fi", "fr", "hr", "hu",
		"it", "lt", "lv", "mt", "nl", "pl", "pt", "ro", "ru", "sk", "sl", "sv", "uk"}},
	{"whisper-large-v3-turbo", "Q5_K_M", 590, true, nil},
	{"Qwen3-ASR-0.6B", "Q5_K_M", 615, false, qwen3Langs},
	{"canary-1b-flash", "Q5_K_M", 733, false, []string{"en", "de", "es", "fr"}},
	{"Qwen3-ASR-1.7B", "Q5_K_M", 1447, false, qwen3Langs},
	{"cohere-transcribe-03-2026", "Q5_K_M", 1688, false, []string{
		"en", "ar", "de", "el", "es", "fr", "it", "ja", "ko", "nl", "pl", "pt", "vi", "zh"}},
	{"granite-speech-4.1-2b-nar", "Q5_K_M", 1699, false, []string{"en", "de", "es", "fr", "pt"}},
	{"canary-qwen-2.5b", "Q5_K_M", 1891, false, []string{"en"}},
}

// qwen3Langs is the set both Qwen3-ASR sizes advertise, which is the same set.
// Nothing writes to a Spec's Langs, so the two entries can share it.
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
