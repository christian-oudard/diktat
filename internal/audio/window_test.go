package audio

import (
	"testing"
	"time"
)

// Below the shortest rehearsed length the graph is a shape no warmup ran, so
// anything shorter has to come out at the floor. Everything above it is handed
// over exactly as it was said.
func TestPadOnlyLiftsShortCaptures(t *testing.T) {
	floor := Warm(0)[0] * SampleRate
	for _, secs := range []float64{0, 0.1, 0.4, 0.99} {
		if got := len(Pad(make([]float32, int(secs*SampleRate)))); got != floor {
			t.Errorf("%gs came out at %d samples, want the %d sample floor", secs, got, floor)
		}
	}
	for _, secs := range []float64{1, 2.5, 30, 61.5} {
		n := int(secs * SampleRate)
		if got := len(Pad(make([]float32, n))); got != n {
			t.Errorf("%gs was padded to %d samples", secs, got)
		}
	}
}

// Padding adds silence and keeps what was said, in that order.
func TestPadKeepsTheAudio(t *testing.T) {
	run := []float32{0.5, -0.5, 0.25}
	padded := Pad(run)
	for i, want := range run {
		if padded[i] != want {
			t.Errorf("sample %d is %v, want %v", i, padded[i], want)
		}
	}
	for _, s := range padded[len(run):] {
		if s != 0 {
			t.Fatalf("padding is not silent")
		}
	}
}

// A model that will not take the whole ladder is rehearsed only at the lengths
// it accepts, since a warmup run it refuses rehearses nothing.
func TestWarmRespectsTheModelsLimit(t *testing.T) {
	warmed := Warm(12 * time.Second)
	if len(warmed) == 0 {
		t.Fatal("no rungs under a 12s limit")
	}
	if top := warmed[len(warmed)-1]; top != 10 {
		t.Errorf("longest rung under a 12s limit is %ds, want 10s", top)
	}
	if len(Warm(0)) != len(Warm(time.Hour)) {
		t.Error("a limit past the ladder dropped rungs")
	}
}
