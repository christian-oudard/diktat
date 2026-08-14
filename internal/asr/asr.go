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
	"time"

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
	// bytes is what this model costs on its device, which is what a cache
	// has to budget against. freeAtLoad is the device's free memory just
	// before it was loaded, kept so Measure can take the difference later.
	bytes      uint64
	freeAtLoad uint64
	// timings is where the last transcription went.
	timings Timings
	// vocabulary biases the decode toward words the model would otherwise
	// get wrong. promptKind is the extension that carries it for this
	// family, or 0 when the family has no way to take one.
	vocabulary string
	promptKind transcribe.ExtKind
}

// Timings is where a transcription's time went. Encode dominates for whisper,
// which runs it over a padded 30 second window however long the utterance.
type Timings struct {
	Mel, Encode, Decode time.Duration
}

// Load opens a GGUF model and keeps it open.
func Load(path string) (*Model, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}
	quiet.Do(func() { transcribe.SetLogHandler(nil) })

	opts, gpu, err := placement()
	if err != nil {
		return nil, err
	}
	before := deviceFree(opts)
	s, err := transcribe.Open(path, opts, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	mm := s.Model()
	m := &Model{
		s:          s,
		name:       strings.TrimSuffix(filepath.Base(path), ".gguf"),
		gpu:        gpu,
		bytes:      uint64(info.Size()),
		freeAtLoad: before,
		promptKind: promptKind(mm),
	}
	m.Measure()
	return m, nil
}

// Bytes is what this model costs on its device: measured against the device's
// free memory where the backend reports it, and the file size otherwise,
// which is a floor rather than an estimate.
func (m *Model) Bytes() uint64 { return m.bytes }

// ErrTruncated means the decode hit the model's output budget before it
// finished. The transcript that comes back is real but incomplete.
//
// An audio-LLM reaches it on input with no speech in it: given noise it has
// nothing to transcribe and generates until the budget runs out. That makes
// it the expected answer to a warmup, and a real problem for an utterance.
var ErrTruncated = transcribe.ErrOutputTruncated

// Timings is where the last transcription's time went.
func (m *Model) Timings() Timings { return m.timings }

// SetVocabulary biases the decode toward these words: jargon, product names
// and acronyms that a general model renders as whatever it knows instead.
// Passed as whisper's initial prompt, which is a nudge rather than a rule, so
// nothing outside the list becomes unsayable.
//
// Only whisper takes it. Everything else in the menu is an encoder-decoder or
// a transducer with no prompt to condition on, and TakesVocabulary says which
// is which so a caller can say so rather than wonder why nothing changed.
func (m *Model) SetVocabulary(hints string) { m.vocabulary = strings.TrimSpace(hints) }

// TakesVocabulary reports whether SetVocabulary does anything for this model.
func (m *Model) TakesVocabulary() bool { return m.promptKind != 0 }

// promptKind is the run extension this model takes a prompt through, or 0.
//
// Two families carry one, by different mechanisms: whisper conditions its
// decoder on the text, voxtral hands it to a language model as an
// instruction. Both halves are probed, the capability and the extension that
// carries it, because voxtral advertised the capability for a while with no
// extension to deliver it.
func promptKind(m *transcribe.Model) transcribe.ExtKind {
	if !m.Supports(transcribe.FeatureInitialPrompt) {
		return 0
	}
	// Whisper conditions its decoder on the words; voxtral is an audio-LLM
	// and is asked to follow an instruction. Both work, and both are worth
	// having, but only where there is speech to transcribe: given silence an
	// instructed audio-LLM invents something and runs to its decode budget.
	// The daemon warms before it applies these, and refuses to transcribe a
	// silent capture, which is what that costs.
	for _, kind := range []transcribe.ExtKind{transcribe.KindWhisperRun, transcribe.KindVoxtralRun} {
		if m.AcceptsExtension(transcribe.SlotRun, kind) {
			return kind
		}
	}
	return 0
}

// promptOptions builds the family's run extension around the hints.
func (m *Model) promptOptions() transcribe.RunExtension {
	switch m.promptKind {
	case transcribe.KindWhisperRun:
		return &transcribe.WhisperRunOptions{InitialPrompt: m.vocabulary}
	case transcribe.KindVoxtralRun:
		// An instruction, since that is what this family takes. Measured
		// with thirty terms over real speech: transcribes correctly.
		return &transcribe.VoxtralRunOptions{
			Instruction: "Transcribe the audio. Expected terms: " + m.vocabulary,
		}
	}
	return nil
}

// Languages are the language codes the model advertises accepting as a hint.
// Empty means it advertises no set, which is not the same as accepting none.
func (m *Model) Languages() ([]string, error) {
	c, err := m.s.Model().Capabilities()
	if err != nil {
		return nil, err
	}
	return c.Languages, nil
}

// Measure re-reads what this model costs and keeps the larger answer.
//
// Worth calling again after the first transcription: loading allocates the
// weights, but ggml allocates its compute buffers when it first runs a graph,
// and on a small model those outweigh the weights several times over. A
// measurement taken at load alone comes back at about the file size, which is
// the wrong number to budget a cache against.
func (m *Model) Measure() {
	if m.freeAtLoad == 0 {
		return // the backend reports no memory; the file size stands
	}
	dev, err := m.s.Model().Device()
	if err != nil || dev.MemoryFree == 0 || dev.MemoryFree >= m.freeAtLoad {
		return
	}
	if used := m.freeAtLoad - dev.MemoryFree; used > m.bytes {
		m.bytes = used
	}
}

// deviceFree is free memory on the device a load with these options will
// land on, or 0 when the backend does not report it.
func deviceFree(opts *transcribe.LoadOptions) uint64 {
	devices, err := transcribe.Devices()
	if err != nil {
		return 0
	}
	i := 0
	if opts != nil {
		i = opts.GPUDevice
	}
	if i < 0 || i >= len(devices) {
		return 0
	}
	return devices[i].MemoryFree
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
			return nil, d.Description, nil
		}
		return &transcribe.LoadOptions{GPUDevice: i}, d.Description, nil
	}
	return &transcribe.LoadOptions{Backend: transcribe.BackendCPU}, "", nil
}

// Name is the model, as the file it was loaded from calls it.
func (m *Model) Name() string { return m.name }

// Arch names the model and the device it runs on, because the difference
// between GPU and CPU here is two orders of magnitude on the encoder and
// falling back is silent. The device is named as the driver describes it, so
// landing on the wrong chip of a hybrid laptop shows up as that chip rather
// than hiding behind a plain "gpu".
func (m *Model) Arch() string {
	where := "cpu"
	if m.gpu != "" {
		where = m.gpu
	}
	return m.name + " on " + where
}

// DeviceMemory is the total memory of the device transcription runs on, or 0
// when the backend reports none. It is what a cache budget is a fraction of.
func DeviceMemory() uint64 {
	quiet.Do(func() { transcribe.SetLogHandler(nil) })
	opts, _, err := placement()
	if err != nil {
		return 0
	}
	devices, err := transcribe.Devices()
	if err != nil {
		return 0
	}
	i := 0
	if opts != nil {
		i = opts.GPUDevice
	}
	if i < 0 || i >= len(devices) {
		return 0
	}
	return devices[i].MemoryTotal
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
	var opts *transcribe.RunOptions
	if m.vocabulary != "" && m.promptKind != 0 {
		opts = &transcribe.RunOptions{Family: m.promptOptions()}
	}
	res, err := m.s.Run(context.Background(), audio, opts)
	if err != nil {
		return "", err
	}
	m.timings = Timings{Mel: res.Timings.Mel, Encode: res.Timings.Encode, Decode: res.Timings.Decode}
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
