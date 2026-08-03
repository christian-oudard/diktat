package audio

import (
	"math"
	"testing"
)

func sine(amp float32, n int) []float32 {
	s := make([]float32, n)
	for i := range s {
		s[i] = amp * float32(math.Sin(2*math.Pi*220*float64(i)/SampleRate))
	}
	return s
}

func TestNormalizeBoostsQuietAudio(t *testing.T) {
	samples := sine(0.01, SampleRate) // well below a normal level
	gain := Normalize(samples)
	if gain <= 1 {
		t.Fatalf("expected gain > 1 for quiet audio, got %.2f", gain)
	}
	if peak, _ := Levels(samples); math.Abs(peak-normTargetPeak) > 0.05 {
		t.Errorf("normalized peak = %.3f, want ~%.2f", peak, normTargetPeak)
	}
}

func TestNormalizeLeavesSilenceUntouched(t *testing.T) {
	samples := make([]float32, 1000) // all zeros
	if gain := Normalize(samples); gain != 1 {
		t.Errorf("silence gain = %.2f, want 1", gain)
	}
}

func TestNormalizeLeavesLoudAudioUntouched(t *testing.T) {
	samples := sine(0.95, SampleRate) // already above the target peak
	if gain := Normalize(samples); gain != 1 {
		t.Errorf("loud gain = %.2f, want 1", gain)
	}
}

func TestNormalizeIgnoresTransients(t *testing.T) {
	// Quiet speech-level body with one loud click: a true-peak normalizer
	// would barely boost, but the percentile ignores the click and lifts the
	// body substantially.
	samples := sine(0.01, SampleRate)
	samples[0] = 0.95
	gain := Normalize(samples)
	if gain < 10 {
		t.Errorf("gain = %.1f, want the body boosted despite the click", gain)
	}
	if peak, _ := Levels(samples); peak > 1 {
		t.Errorf("peak after normalize = %.3f, want <= 1", peak)
	}
}
