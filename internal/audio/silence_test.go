package audio

import (
	"math"
	"testing"
)

// A capture with no speech in it must not reach a model. An audio-LLM asked
// to transcribe silence invents something and runs to its decode budget.
func TestIsSilent(t *testing.T) {
	n := 3 * SampleRate
	speech := make([]float32, n)
	for i := range speech {
		speech[i] = float32(0.3 * math.Sin(float64(i)*0.05))
	}
	quiet := make([]float32, n)
	for i := range speech {
		quiet[i] = speech[i] / 20 // 26 dB down; still words
	}
	// The daemon's own warmup pattern, which is what exposed this.
	noise := make([]float32, 4*SampleRate)
	for i := range noise {
		noise[i] = float32((i%17)-8) / 8000
	}

	for _, c := range []struct {
		name string
		pcm  []float32
		want bool
	}{
		{"speech", speech, false},
		{"quiet speech", quiet, false},
		{"warmup noise", noise, true},
		{"digital silence", make([]float32, n), true},
		{"empty", nil, true},
	} {
		if got := IsSilent(c.pcm); got != c.want {
			peak, rms := Levels(c.pcm)
			t.Errorf("IsSilent(%s) = %v, want %v (peak %.4f rms %.5f)", c.name, got, c.want, peak, rms)
		}
	}
}
