package audio

import (
	"math"
	"testing"
)

// A dead input and a quiet room are different questions, and IsSilent answers
// only the second. That was the daemon's whole vocabulary once, so a headset
// whose link had died read the same as someone thinking about what to say,
// and the daemon typed nothing for the rest of the session.
func TestIsDead(t *testing.T) {
	n := 3 * SampleRate
	speech := make([]int16, n)
	for i := range speech {
		speech[i] = int16(0.3 * full * math.Sin(float64(i)*0.05))
	}
	// What a Jabra headset delivers on a healthy link: its DSP gates the
	// silence between words to bit-exact zero, so most of a real capture is
	// zeros and only the speech carries any bits at all. Measured at 9.7%
	// nonzero over 22 seconds, with zero runs up to 6.2s. A capture like this
	// is alive, and the whole reason IsDead reads the entire capture rather
	// than watching for a span of quiet.
	gated := make([]int16, n)
	copy(gated[n/2:], speech[:n/10])

	for _, c := range []struct {
		name string
		pcm  []int16
		want bool
	}{
		{"speech", speech, false},
		{"gated silence around speech", gated, false},
		{"one set bit", append(make([]int16, n-1), 1), false},
		{"digital silence", make([]int16, n), true},
		{"nothing captured", nil, true},
	} {
		if got := IsDead(c.pcm); got != c.want {
			t.Errorf("IsDead(%s) = %v, want %v", c.name, got, c.want)
		}
	}

	// The two checks are meant to disagree here: a gated capture holding
	// speech is silent by neither reading, but a threshold alone would not
	// tell a dead device from a quiet one.
	if IsSilent(gated) {
		t.Error("IsSilent(gated silence around speech) = true, want false")
	}
}
