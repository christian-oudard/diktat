package asr

// Whisper runs whisper.cpp in process. The model is loaded once and stays
// loaded, the same as moonshine, rather than being re-read per utterance.
//
// Only four calls are needed, so this binds them directly instead of pulling
// in whisper.cpp's own Go bindings.

/*
#cgo LDFLAGS: -lwhisper -lggml -lggml-base
#include <stdlib.h>
#include <whisper.h>
#include <ggml-backend.h>

// whisper_full takes params by value, and cgo cannot build a struct literal
// with C function pointers in it, so set up the params on the C side.
static int diktat_full(struct whisper_context *ctx, float *samples, int n, int threads) {
    struct whisper_full_params p = whisper_full_default_params(WHISPER_SAMPLING_GREEDY);
    p.n_threads         = threads;
    p.translate         = false;
    p.no_context        = true;   // each utterance stands alone
    p.single_segment    = false;
    p.print_progress    = false;
    p.print_realtime    = false;
    p.print_timestamps  = false;
    p.print_special     = false;
    p.suppress_blank    = true;
    p.suppress_nst      = true;   // no [BLANK_AUDIO], (music), and friends
    return whisper_full(ctx, p, samples, n);
}

// whisper.cpp and ggml narrate model loading to stderr. The daemon has its own
// log and the offline tools print results, so drop it.
static void diktat_quiet(enum ggml_log_level level, const char *text, void *user) {
    (void)level; (void)text; (void)user;
}

// Silence both before loading backends, which narrates too.
static void diktat_silence(void) {
    whisper_log_set(diktat_quiet, NULL);
    ggml_log_set(diktat_quiet, NULL);
}

static struct whisper_context *diktat_init(const char *path, int use_gpu) {
    struct whisper_context_params cp = whisper_context_default_params();
    cp.use_gpu = use_gpu;
    return whisper_init_from_file_with_params(path, cp);
}
*/
import "C"

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"unsafe"
)

// ggml keeps each compute backend in its own shared library and loads them at
// runtime. Nothing finds them by default from a Go binary, so point it at the
// directory the nix wrapper names. Without this ggml registers no backend at
// all and aborts inside whisper_init.
var loadBackends sync.Once

func initGGML() {
	loadBackends.Do(func() {
		C.diktat_silence()
		dir := os.Getenv("GGML_BACKEND_DIR")
		if dir == "" {
			C.ggml_backend_load_all()
			return
		}
		cDir := C.CString(dir)
		defer C.free(unsafe.Pointer(cDir))
		C.ggml_backend_load_all_from_path(cDir)
	})
}

type Whisper struct {
	ctx     *C.struct_whisper_context
	name    string
	threads int
}

// LoadWhisper opens a ggml model and keeps it open.
func LoadWhisper(modelPath string) (*Whisper, error) {
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("whisper model: %w", err)
	}
	initGGML()

	cPath := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cPath))

	// CPU only for now; GPU backends would be loaded the same way.
	ctx := C.diktat_init(cPath, C.int(0))
	if ctx == nil {
		return nil, fmt.Errorf("whisper: cannot load %s", modelPath)
	}
	return &Whisper{
		ctx:     ctx,
		name:    strings.TrimSuffix(filepath.Base(modelPath), ".bin"),
		threads: runtime.NumCPU(),
	}, nil
}

func (w *Whisper) Arch() string {
	return fmt.Sprintf("whisper.cpp %s, %d threads", w.name, w.threads)
}

func (w *Whisper) Close() {
	if w.ctx != nil {
		C.whisper_free(w.ctx)
		w.ctx = nil
	}
}

// Transcribe runs the model over 16 kHz mono samples. Whisper pads every
// utterance to a 30 second window internally, so the cost is roughly flat
// regardless of how long the utterance actually is.
func (w *Whisper) Transcribe(audio []float32) (string, error) {
	if len(audio) == 0 {
		return "", nil
	}
	rc := C.diktat_full(w.ctx, (*C.float)(unsafe.Pointer(&audio[0])), C.int(len(audio)), C.int(w.threads))
	if rc != 0 {
		return "", fmt.Errorf("whisper_full: %d", int(rc))
	}
	// Keep audio alive across the call, since C holds the pointer.
	runtime.KeepAlive(audio)

	var b strings.Builder
	for i := C.int(0); i < C.whisper_full_n_segments(w.ctx); i++ {
		b.WriteString(C.GoString(C.whisper_full_get_segment_text(w.ctx, i)))
	}
	return dropAnnotations(b.String()), nil
}

// dropAnnotations removes whisper's non-speech markers. Even with suppress_nst
// it still emits things like [BLANK_AUDIO], (wind blowing) or a bare musical
// note for audio it hears no words in. Moonshine returns an empty string for
// the same input, and the daemon only skips typing on empty, so without this a
// silent capture types "[BLANK_AUDIO]" into whatever has focus.
func dropAnnotations(text string) string {
	var kept []string
	for _, field := range strings.Fields(text) {
		kept = append(kept, field)
	}
	out := strings.Join(kept, " ")
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
