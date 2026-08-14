package audio

import (
	"math"
	"testing"
)

// A capture with no speech in it must not reach a model. An audio-LLM asked
// to transcribe silence invents something and runs to its decode budget.
func TestIsSilent(t *testing.T) {
	n := 3 * SampleRate
	speech := make([]int16, n)
	for i := range speech {
		speech[i] = int16(0.3 * full * math.Sin(float64(i)*0.05))
	}
	quiet := make([]int16, n)
	for i := range speech {
		quiet[i] = speech[i] / 20 // 26 dB down; still words
	}
	// The signal the warmup used to use, which is what exposed this.
	noise := make([]int16, 4*SampleRate)
	for i := range noise {
		noise[i] = int16(float64((i%17)-8) / 8000 * full)
	}

	for _, c := range []struct {
		name string
		pcm  []int16
		want bool
	}{
		{"speech", speech, false},
		{"quiet speech", quiet, false},
		{"warmup noise", noise, true},
		{"digital silence", make([]int16, n), true},
		{"empty", nil, true},
	} {
		if got := IsSilent(c.pcm); got != c.want {
			peak, rms := Levels(c.pcm)
			t.Errorf("IsSilent(%s) = %v, want %v (peak %.4f rms %.5f)", c.name, got, c.want, peak, rms)
		}
	}
}
