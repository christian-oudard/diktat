package audio

import (
	"math"
	"testing"
)

// tone is a float32 test signal, which is the form Resample works in: it sits
// on the synthesiser's output, not on the capture path.
func tone(hz float64, rate, n int) []float32 {
	s := make([]float32, n)
	for i := range s {
		s[i] = float32(math.Sin(2 * math.Pi * hz * float64(i) / float64(rate)))
	}
	return s
}

// Resample exists for one caller, espeak's 22050 Hz output, so what matters is
// that the result is the right length and still the same tone.
func TestResampleLength(t *testing.T) {
	got := Resample(tone(220, 22050, 22050), 22050, SampleRate)
	if len(got) != SampleRate {
		t.Errorf("Resample gave %d samples, want %d", len(got), SampleRate)
	}
}

// A rate that is already right is not worth a pass over the samples.
func TestResampleSameRate(t *testing.T) {
	in := tone(220, SampleRate, 100)
	if got := Resample(in, SampleRate, SampleRate); &got[0] != &in[0] {
		t.Error("Resample copied when the rate already matched")
	}
}

// The frequency has to survive, or the model is warmed on something that is
// not speech any more.
func TestResampleKeepsPitch(t *testing.T) {
	const hz = 220
	got := Resample(tone(hz, 22050, 22050), 22050, SampleRate)
	crossings := 0
	for i := 1; i < len(got); i++ {
		if (got[i-1] < 0) != (got[i] < 0) {
			crossings++
		}
	}
	if want := 2 * hz; math.Abs(float64(crossings-want)) > 2 {
		t.Errorf("%d zero crossings in a second, want about %d", crossings, want)
	}
}
