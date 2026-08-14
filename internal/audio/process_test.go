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

// peakAfter is the peak of a capture once the gain is applied, which is what
// the model actually sees.
func peakAfter(samples []int16, gain float64) float64 {
	var peak float64
	for _, s := range Floats(samples, gain) {
		if a := math.Abs(float64(s)); a > peak {
			peak = a
		}
	}
	return peak
}

func TestGainBoostsQuietAudio(t *testing.T) {
	samples := sine(0.01, SampleRate) // well below a normal level
	gain := Gain(samples)
	if gain <= 1 {
		t.Fatalf("expected gain > 1 for quiet audio, got %.2f", gain)
	}
	if peak := peakAfter(samples, gain); math.Abs(peak-normTargetPeak) > 0.05 {
		t.Errorf("normalized peak = %.3f, want ~%.2f", peak, normTargetPeak)
	}
}

func TestGainLeavesSilenceUntouched(t *testing.T) {
	samples := make([]int16, 1000) // all zeros
	if gain := Gain(samples); gain != 1 {
		t.Errorf("silence gain = %.2f, want 1", gain)
	}
}

func TestGainLeavesLoudAudioUntouched(t *testing.T) {
	samples := sine(0.95, SampleRate) // already above the target peak
	if gain := Gain(samples); gain != 1 {
		t.Errorf("loud gain = %.2f, want 1", gain)
	}
}

func TestGainIgnoresTransients(t *testing.T) {
	// Quiet speech-level body with one loud click: a true-peak normalizer
	// would barely boost, but the percentile ignores the click and lifts the
	// body substantially.
	samples := sine(0.01, SampleRate)
	click := 0.95 * float64(full)
	samples[0] = int16(click)
	gain := Gain(samples)
	if gain < 10 {
		t.Errorf("gain = %.1f, want the body boosted despite the click", gain)
	}
	if peak := peakAfter(samples, gain); peak > 1 {
		t.Errorf("peak after gain = %.3f, want <= 1", peak)
	}
}

// The capture's own form round-trips: what the offline tools convert into
// int16 and back has to be the same audio the model would have heard.
func TestIntsFloatsRoundTrip(t *testing.T) {
	in := []float32{0, 0.5, -0.5, 0.999, -0.999}
	out := Floats(Ints(in), 1)
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
