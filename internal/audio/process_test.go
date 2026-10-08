package audio

import (
	"math"
	"testing"
)

// sine is a test tone at the amplitude given on the [0, 1] scale, in the
// 16-bit form a capture arrives in.
func sine(amp float64, n int) []int16 {
	s := make([]int16, n)
	for i := range s {
		s[i] = int16(amp * full * math.Sin(2*math.Pi*220*float64(i)/SampleRate))
	}
	return s
}

// The capture's own form round-trips: what the offline tools convert into
// int16 and back has to be the same audio the model would have heard.
func TestIntsFloatsRoundTrip(t *testing.T) {
	in := []float32{0, 0.5, -0.5, 0.999, -0.999}
	out := Floats(Ints(in))
	for i := range in {
		if math.Abs(float64(out[i]-in[i])) > 1e-4 {
			t.Errorf("sample %d round-tripped %v to %v", i, in[i], out[i])
		}
	}
}

// A capture device that delivers faster than real time must not grow the
// buffer without bound; the daemon's wall-clock stop cannot be relied on for
// that. Observed with an ALSA null device, which buffered 19279s of audio in
// 4s of wall clock and drove onnxruntime to a 20 TB allocation.
func TestAppendSamplesCapsBuffer(t *testing.T) {
	// Shrunk for the test: the real guard is an hour, and allocating 230 MB
	// to prove a bounds check would be the slowest test in the tree.
	defer func(n int) { maxSamples = n }(maxSamples)
	maxSamples = 5 * SampleRate

	r := &Recorder{}
	chunk := make([]int16, SampleRate)
	for i := 0; i < 2*maxSamples/SampleRate+10; i++ {
		r.appendSamples(chunk)
	}
	if len(r.buf) != maxSamples {
		t.Errorf("buffer is %d samples, want the cap of %d", len(r.buf), maxSamples)
	}
}

func TestAppendSamplesPartialFinalChunk(t *testing.T) {
	defer func(n int) { maxSamples = n }(maxSamples)
	maxSamples = 5 * SampleRate

	r := &Recorder{}
	r.appendSamples(make([]int16, maxSamples-10))
	r.appendSamples(make([]int16, 100))
	if len(r.buf) != maxSamples {
		t.Errorf("buffer is %d samples, want the cap of %d", len(r.buf), maxSamples)
	}
}

func TestDrainKeepsEverythingPastTheCap(t *testing.T) {
	defer func(n int) { maxSamples = n }(maxSamples)
	maxSamples = 5 * SampleRate

	r := &Recorder{}
	r.Start()
	chunk := make([]int16, SampleRate)
	total := 0
	for i := 0; i < 3*maxSamples/SampleRate; i++ {
		r.appendSamples(chunk)
		total += len(r.Drain())
	}
	total += len(r.Stop())
	if want := 3 * maxSamples; total != want {
		t.Errorf("drained %d samples, want all %d", total, want)
	}
}
