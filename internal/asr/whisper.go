package asr

// Whisper runs whisper.cpp in process. The model is loaded once and stays
// loaded, the same as moonshine, rather than being re-read per utterance.
//
// Only four calls are needed, so this binds them directly instead of pulling
// in whisper.cpp's own Go bindings.

/*
#cgo LDFLAGS: -lwhisper -lggml -lggml-base
#include <stdio.h>
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

// whisper takes the first device that is a GPU *or* an integrated GPU, so on a
// hybrid laptop it can land on the Intel chip rather than the discrete card.
// An iGPU shares memory bandwidth with the CPU it would be replacing and is no
// clear win, so find the discrete one and pin it. Returns the index in
// whisper's own numbering, which counts both kinds, or -1 for none.
static int diktat_discrete_gpu(void) {
    int index = 0;
    for (size_t i = 0; i < ggml_backend_dev_count(); i++) {
        enum ggml_backend_dev_type t = ggml_backend_dev_type(ggml_backend_dev_get(i));
        if (t == GGML_BACKEND_DEVICE_TYPE_GPU) {
            return index;
        }
        if (t == GGML_BACKEND_DEVICE_TYPE_IGPU) {
            index++;
        }
    }
    return -1;
}

// Every device ggml registered, as "name (type)" lines, so a machine with both
// an integrated and a discrete GPU can be checked at a glance rather than
// trusted. Types are 0 cpu, 1 gpu, 2 igpu, 3 accel.
static void diktat_devices(char *out, size_t n) {
    size_t used = 0;
    out[0] = '\0';
    for (size_t i = 0; i < ggml_backend_dev_count() && used + 1 < n; i++) {
        ggml_backend_dev_t d = ggml_backend_dev_get(i);
        int w = snprintf(out + used, n - used, "%s%s (type %d)",
                         used ? ", " : "",
                         ggml_backend_dev_description(d),
                         (int)ggml_backend_dev_type(d));
        if (w < 0) break;
        used += (size_t)w;
    }
}

static struct whisper_context *diktat_init(const char *path, int gpu_device) {
    struct whisper_context_params cp = whisper_context_default_params();
    cp.use_gpu    = gpu_device >= 0;
    cp.gpu_device = gpu_device < 0 ? 0 : gpu_device;
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
	"strconv"
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
	gpu     bool
}

// LoadWhisper opens a ggml model and keeps it open.
func LoadWhisper(modelPath string) (*Whisper, error) {
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("whisper model: %w", err)
	}
	initGGML()

	cPath := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cPath))

	device := gpuDevice()
	ctx := C.diktat_init(cPath, C.int(device))
	if ctx == nil {
		return nil, fmt.Errorf("whisper: cannot load %s", modelPath)
	}
	return &Whisper{
		ctx:     ctx,
		name:    strings.TrimSuffix(filepath.Base(modelPath), ".bin"),
		threads: runtime.NumCPU(),
		gpu:     device >= 0,
	}, nil
}

// gpuDevice is the device index to transcribe on, or -1 to stay on the CPU.
// DIKTAT_GPU overrides the choice: 0 forces CPU, 1 takes whatever whisper would
// have picked on its own, including an integrated GPU.
func gpuDevice() int {
	if v, err := strconv.ParseBool(os.Getenv("DIKTAT_GPU")); err == nil {
		if v {
			return 0
		}
		return -1
	}
	return int(C.diktat_discrete_gpu())
}

// Arch names the device, because the difference between GPU and CPU here is
// two orders of magnitude on the encoder, and falling back is silent. It lists
// everything ggml found, so picking the wrong chip on a hybrid laptop shows up
// rather than hiding behind a plain "gpu".
func (w *Whisper) Arch() string {
	where := fmt.Sprintf("cpu, %d threads", w.threads)
	if w.gpu {
		where = "gpu"
	}
	return fmt.Sprintf("whisper.cpp %s, %s [%s]", w.name, where, Devices())
}

// Devices describes every compute device ggml registered.
func Devices() string {
	initGGML()
	var buf [1024]C.char
	C.diktat_devices(&buf[0], C.size_t(len(buf)))
	return C.GoString(&buf[0])
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
