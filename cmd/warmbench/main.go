// Command warmbench measures what a warmup run buys. Loading a model is not
// the same as being ready to use it: ggml allocates its compute buffers on the
// first graph run, and the Vulkan backend compiles shaders there too. The
// daemon pays that on a throwaway transcription so the user does not pay it on
// their first utterance, and this answers the two questions that design rests
// on. Does the content of the warm audio matter, or only its shape? And does
// warming at one length cover a probe at another?
//
// It runs one warm strategy per process, because both costs are cached in
// process: a second strategy in the same run would inherit the first one's
// work and measure nothing. Drive the matrix from a shell loop, and for the
// cold-driver case point __GL_SHADER_DISK_CACHE_PATH at an empty directory.
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	transcribe "github.com/handy-computer/transcribe.cpp/bindings/go"

	"github.com/christian-oudard/diktat/internal/asr"
	"github.com/christian-oudard/diktat/internal/audio"
	"github.com/christian-oudard/diktat/internal/human"
	"github.com/christian-oudard/diktat/internal/models"
	"github.com/christian-oudard/diktat/internal/wav"
)

func main() {
	fs := flag.NewFlagSet("warmbench", flag.ExitOnError)
	name := fs.String("model", "parakeet-tdt_ctc-110m", "model to load")
	warm := fs.String("warm", "sawtooth:4", "warm runs as kind:seconds, comma separated; none for no warmup")
	probe := fs.String("probe", "2,5,10,20", "probe lengths in seconds")
	passes := fs.Int("passes", 2, "runs per probe length; the second is the steady state")
	clip := fs.String("clip", "", "wav of real speech, looped to length for probes")
	warmClip := fs.String("warmclip", "", "wav to warm on, when it should differ from the probes")
	pad := fs.Bool("pad", true, "round probes up to a warmed length, as the daemon does")
	// Dictation arrives in bursts with quiet between, where a bench runs
	// flat out. If a card drops its clocks when idle, only the first shape
	// tells them apart.
	idle := fs.Duration("idle", 0, "wait this long before each probe, as a user pausing does")
	// The daemon knows a transcription is coming as soon as recording starts,
	// which is seconds before the audio is ready. This asks whether spending
	// that time on a throwaway run is worth it.
	ramp := fs.Duration("ramp", 0, "run this much throwaway audio after idling, before the probe")
	fs.Parse(os.Args[1:])

	path := models.Resolve(*name)
	if err := models.Check(path); err != nil {
		log.Fatalf("%s is not downloaded. Get it with:\n  diktat model %s", *name, *name)
	}
	// Warming on one voice and probing with another is the point of the
	// second flag: it says whether what the warmup was fed has to resemble
	// what the user will say.
	speech := readClip(*clip)
	warmSpeech := speech
	if *warmClip != "" {
		warmSpeech = readClip(*warmClip)
	}

	t0 := time.Now()
	model, err := asr.Load(path)
	if err != nil {
		log.Fatal(err)
	}
	defer model.Close()
	fmt.Printf("# %s\n", model.Arch())
	if devs, err := transcribe.Devices(); err == nil {
		for i, d := range devs {
			fmt.Printf("# device %d: %s (%s)\n", i, d.Description, d.Type)
		}
	}
	fmt.Printf("# load %s, %s resident before any graph has run\n",
		time.Since(t0).Round(time.Millisecond), human.Bytes(model.Bytes()))
	fmt.Println("stage\tkind\tsecs\tpass\twall_ms\tmel_ms\tencode_ms\tdecode_ms\tother_ms\tresident\tkernels\tbuilt")

	for _, spec := range strings.Split(*warm, ",") {
		kind, secs := parseSpec(spec)
		if kind == "none" {
			continue
		}
		// Warm runs are not padded: the point of the flag is to say exactly
		// what shape was rehearsed.
		run(model, "warm", kind, secs, 1, warmSpeech, false)
	}
	for _, field := range strings.Split(*probe, ",") {
		secs, err := strconv.ParseFloat(strings.TrimSpace(field), 64)
		if err != nil {
			log.Fatalf("probe %q: %v", field, err)
		}
		// The probe is speech where there is a clip, because a decode that
		// emits nothing exercises none of the decoder's length.
		kind := "speech"
		if speech == nil {
			kind = "sawtooth"
		}
		for pass := 1; pass <= *passes; pass++ {
			time.Sleep(*idle)
			if *ramp > 0 {
				run(model, "ramp", kind, ramp.Seconds(), pass, speech, *pad)
			}
			// Probes go through the padding the daemon applies, so a probe
			// asks what an utterance of that length costs rather than what a
			// shape the daemon never uses would cost.
			run(model, "probe", kind, secs, pass, speech, *pad)
		}
	}
}

// run transcribes one generated clip and reports where the time went. Wall
// time is the number that matters to the user; encode and decode say which
// half of the model was cold, and resident says whether ggml had to allocate.
//
// The last column is what this length asked the backend to build that no
// earlier one had. Names rather than a count, because they say what was new:
// mul_mat variants carry their tile size and whether the dimensions took the
// aligned path, which is the band structure the buckets have to cover.
func run(m *asr.Model, stage, kind string, secs float64, pass int, speech []float32, pad bool) {
	clip := material(kind, secs, speech)
	if pad {
		clip = audio.Pad(clip)
	}
	// Kernels are only ever appended, so what this run built is the tail.
	kernels := m.CompiledKernelNames()
	t0 := time.Now()
	if _, err := m.Transcribe(clip); err != nil {
		log.Fatalf("%s %s: %v", stage, kind, err)
	}
	wall := time.Since(t0)
	compiled := m.CompiledKernelNames()[len(kernels):]
	t := m.Timings()
	fmt.Printf("%s\t%s\t%g\t%d\t%d\t%d\t%d\t%d\t%d\t%s\t%d\t%s\n", stage, kind, secs, pass,
		wall.Milliseconds(), t.Mel.Milliseconds(), t.Encode.Milliseconds(),
		t.Decode.Milliseconds(), t.Other.Milliseconds(), human.Bytes(m.Bytes()),
		len(compiled), strings.Join(compiled, " "))
}

// material makes secs of audio of the named kind. The four kinds are the
// candidates for what a warmup should be fed: nothing at all, the faint
// deterministic wobble the daemon uses, white noise at the same level, and
// real speech looped to length.
func material(kind string, secs float64, speech []float32) []float32 {
	n := int(secs * audio.SampleRate)
	buf := make([]float32, n)
	switch kind {
	case "silence":
	case "sawtooth":
		for i := range buf {
			buf[i] = float32((i%17)-8) / 8000
		}
	case "noise":
		// An LCG rather than math/rand, so one run is comparable with the
		// next and with the sawtooth it is being weighed against: same
		// amplitude, different spectrum.
		state := uint32(1)
		for i := range buf {
			state = state*1664525 + 1013904223
			buf[i] = (float32(state>>8)/float32(1<<24) - 0.5) / 500
		}
	case "loudnoise":
		state := uint32(1)
		for i := range buf {
			state = state*1664525 + 1013904223
			buf[i] = (float32(state>>8)/float32(1<<24) - 0.5) / 5
		}
	case "speech":
		if speech == nil {
			log.Fatal("speech needs -clip")
		}
		for i := range buf {
			buf[i] = speech[i%len(speech)]
		}
	default:
		log.Fatalf("unknown kind %q", kind)
	}
	return buf
}

// readClip loads a wav at the pipeline's rate, or returns nil for no path.
func readClip(path string) []float32 {
	if path == "" {
		return nil
	}
	s, rate, err := wav.ReadWAV(path)
	if err != nil {
		log.Fatal(err)
	}
	if rate != audio.SampleRate {
		log.Fatalf("%s is %d Hz, want %d", path, rate, audio.SampleRate)
	}
	return s
}

func parseSpec(spec string) (string, float64) {
	kind, secs, ok := strings.Cut(strings.TrimSpace(spec), ":")
	if !ok {
		return kind, math.NaN()
	}
	f, err := strconv.ParseFloat(secs, 64)
	if err != nil {
		log.Fatalf("warm %q: %v", spec, err)
	}
	return kind, f
}
