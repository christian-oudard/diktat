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
	samples := sine(0.01, SampleRate) // well below the transcription threshold
	_, rms := Levels(samples)
	gain := Normalize(samples, rms)
	if gain <= 1 {
		t.Fatalf("expected gain > 1 for quiet audio, got %.2f", gain)
	}
	_, rms2 := Levels(samples)
	if math.Abs(rms2-normTargetRMS) > 0.005 {
		t.Errorf("normalized rms = %.4f, want ~%.4f", rms2, normTargetRMS)
	}
}

func TestNormalizeLeavesSilenceUntouched(t *testing.T) {
	samples := make([]float32, 1000) // all zeros
	if gain := Normalize(samples, 0); gain != 1 {
		t.Errorf("silence gain = %.2f, want 1", gain)
	}
}

func TestNormalizeClipsSpikyAudio(t *testing.T) {
	// Near-silent body with one loud transient: low rms, high peak.
	samples := make([]float32, SampleRate)
	for i := range samples {
		samples[i] = 0.005
	}
	samples[0] = 0.9
	_, rms := Levels(samples)
	Normalize(samples, rms)
	if peak, _ := Levels(samples); peak > 1 {
		t.Errorf("peak after normalize = %.3f, want <= 1", peak)
	}
}
