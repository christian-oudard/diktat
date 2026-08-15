// Package warmup rehearses a model at every length a warmup covers.
//
// Loading a model is not the same as being ready to use it: the Vulkan backend
// defers compiling its shaders, and ggml defers allocating its compute
// buffers, to the first graph run of each shape, and the shape follows the
// length of the audio.
//
// Shared rather than kept in the daemon because it is also what tells a model
// how much audio it can take in one graph, which is measured from the lengths
// it has run. A tool that skips warming gets the unmeasured floor instead and
// cuts audio the daemon would not, which is the difference between measuring
// the daemon's pipeline and measuring something near it.
package warmup

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/christian-oudard/diktat/internal/asr"
	"github.com/christian-oudard/diktat/internal/audio"
	"github.com/christian-oudard/diktat/internal/wav"
)

// Run rehearses each bucket in internal/audio and reports what each one cost
// and compiled, or "" for a model with nothing to warm.
//
// Per bucket rather than a total, since the total cannot say which bucket was
// worth running. A bucket that compiles nothing on every model is one to drop,
// and a bucket that compiles on a machine where they were never measured is
// the set being too sparse for that GPU.
//
// There is nothing to warm on the CPU: with no shaders to compile, seven warm
// strategies measured identically to no warmup at all.
func Run(m *asr.Model) (string, error) {
	if !m.OnGPU() {
		return "", nil
	}
	speech, err := Speech()
	if err != nil {
		return "", err
	}
	buckets := audio.Warm(m.MaxAudio())
	work := make([]string, 0, len(buckets))
	before := m.CompiledKernels()
	for _, secs := range buckets {
		started := time.Now()
		// A truncated warmup is a success: the graph ran, which is the entire
		// point, and the transcript is thrown away either way. An audio-LLM
		// can talk its way to the decode budget on a rehearsal, and giving up
		// there would leave the cache budgeting the largest models by their
		// file size.
		if _, err := m.Transcribe(Fit(speech, secs)); err != nil && !errors.Is(err, asr.ErrTruncated) {
			return strings.Join(work, " "), fmt.Errorf("%ds: %w", secs, err)
		}
		compiled := m.CompiledKernels() - before
		before += compiled
		work = append(work, fmt.Sprintf("%ds:%s/%dk", secs,
			time.Since(started).Round(time.Millisecond), compiled))
	}
	return strings.Join(work, " "), nil
}

// sentences are the Harvard sentences from docs/mic-calibration.md, which are
// phonetically balanced and already in the repo for the microphone check.
const sentences = "The birch canoe slid on the smooth planks. " +
	"Glue the sheet to the dark blue background. " +
	"These days a chicken leg is a rare dish. " +
	"The juice of lemons makes fine punch. " +
	"A pod of whales sped past the quiet cove."

// Speech renders those sentences once and keeps them, since every load this
// session warms on the same audio and the buffer is under 2 MB.
//
// It rehearses on speech rather than on a signal because half the cost is in
// the decoder, whose shapes follow the number of tokens that come out. Noise
// emits none, so under it a 20s utterance on granite still paid 5.3s of
// decode; on speech, 57ms.
//
// Synthesised rather than shipped: a wav in the repo is data, and this only
// needs audio that provokes a realistic number of tokens. Measured, espeak-ng
// and a neural synthesiser warmed identically, 725ms against 722ms on the
// probe that matters, so the cheap one wins.
var Speech = sync.OnceValues(func() ([]float32, error) {
	// espeak-ng renders at its voice's rate whatever we ask for, so the rate
	// comes back from the header rather than being assumed.
	wave, err := exec.Command("espeak-ng", "-v", "en-us", "-s", "150", "--stdout", sentences).Output()
	if err != nil {
		return nil, fmt.Errorf("espeak-ng: %w", err)
	}
	samples, rate, err := wav.Decode(wave)
	if err != nil {
		return nil, err
	}
	return audio.Resample(samples, rate, audio.SampleRate), nil
})

// Fit cuts or loops speech to exactly secs of audio. Looping is honest here:
// the bucket is about the shape of the graph, and a repeated sentence produces
// tokens at the same rate as a longer one would.
func Fit(speech []float32, secs int) []float32 {
	out := make([]float32, secs*audio.SampleRate)
	for i := range out {
		out[i] = speech[i%len(speech)]
	}
	return out
}
