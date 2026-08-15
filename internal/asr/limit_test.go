package asr

import (
	"testing"
	"time"
)

const gib = 1 << 30

// A clip no longer than one already run needs no allocation, since the compute
// buffers for it are held. That makes the longest clip so far the floor, and a
// device with nothing left to spare still takes it rather than refusing every
// length.
func TestALengthAlreadyRunIsAlwaysAllowed(t *testing.T) {
	for _, spare := range []uint64{0, 1, gib} {
		if got := fitsIn(spare, gib, 30*time.Second); got < 30*time.Second {
			t.Errorf("with %d bytes spare the limit is %s, under the 30s already run", spare, got)
		}
	}
}

// The spare memory buys audio at the rate the audio already run cost.
func TestSpareMemoryBuysAudioAtTheMeasuredRate(t *testing.T) {
	// 1 GiB bought 30s, so 2 GiB spare buys 60s more.
	if got, want := fitsIn(2*gib, gib, 30*time.Second), 90*time.Second; got != want {
		t.Errorf("limit is %s, want %s", got, want)
	}
	// Half the cost per second, so the same spare buys twice the audio.
	if got, want := fitsIn(2*gib, gib, 60*time.Second), 180*time.Second; got != want {
		t.Errorf("limit is %s, want %s", got, want)
	}
}

// Nothing has been measured before the first run, and on a backend that
// reports no memory nothing ever is. Neither may pass for a limit of zero,
// which would cut the audio into nothing.
func TestNoMeasurementImposesNoLimit(t *testing.T) {
	if got := fitsIn(gib, 0, 30*time.Second); got != 0 {
		t.Errorf("an unmeasured graph gave a %s limit", got)
	}
	if got := fitsIn(gib, gib, 0); got != 0 {
		t.Errorf("a model that has run nothing gave a %s limit", got)
	}
}

// Two limits, either of which may be absent. What the model says it accepts
// and what the card can hold are independent, and the tighter one wins:
// granite claims six and a half minutes and dies at three, while
// canary-180m-flash claims nothing and dies at five.
func TestTheTighterLimitWins(t *testing.T) {
	minute := time.Minute
	cases := []struct{ claim, fit, want time.Duration }{
		{0, 0, 0},
		{0, 3 * minute, 3 * minute},
		{6 * minute, 0, 6 * minute},
		{6 * minute, 3 * minute, 3 * minute},
		{3 * minute, 6 * minute, 3 * minute},
	}
	for _, c := range cases {
		if got := shorter(c.claim, c.fit); got != c.want {
			t.Errorf("claim %s and fit %s gave %s, want %s", c.claim, c.fit, got, c.want)
		}
	}
}
