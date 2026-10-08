package main

import (
	"math"
	"strings"
	"testing"

	"github.com/christian-oudard/diktat/internal/audio"
)

// tone is a square wave at rms, which for a square wave is also its amplitude.
func tone(seconds float64, rms float64) []int16 {
	s := make([]int16, int(seconds*audio.SampleRate))
	for i := range s {
		v := int16(rms * 32767)
		if i%2 == 1 {
			v = -v
		}
		s[i] = v
	}
	return s
}

func db(d float64) float64 { return math.Pow(10, d/20) }

func TestHeight(t *testing.T) {
	for _, c := range []struct {
		level float64
		want  int
	}{
		{math.Inf(-1), 0},
		{-80, 0},
		{-46, 0},
		{-44, 1},
		{-26, 4},
		{-6, 8},
		{0, 8},
	} {
		if got := height(c.level); got != c.want {
			t.Errorf("height(%v) = %d, want %d", c.level, got, c.want)
		}
	}
}

func TestTraceDrawsOneBlockPerFrame(t *testing.T) {
	var out strings.Builder
	tr := newTrace(&out)
	tr.add(sine(-80))
	tr.add(tone(0.1, 0))
	tr.add(tone(0.1, db(-6)))
	tr.add(tone(0.15, db(-26)))
	if got := out.String(); got != "  █▄" {
		t.Errorf("got %q, want %q", got, "  █▄")
	}
	// The half frame left over is drawn once the rest of it arrives.
	tr.add(tone(0.05, db(-26)))
	if got := out.String(); got != "  █▄▄" {
		t.Errorf("got %q, want %q", got, "  █▄▄")
	}
}

// A block shows only its own 100 ms: silence straight after a loud block is
// drawn blank.
func TestTraceCarriesNothingOver(t *testing.T) {
	var out strings.Builder
	tr := newTrace(&out)
	tr.add(sine(-80))
	tr.add(tone(0.1, db(-6)))
	tr.add(tone(0.1, 0))
	tr.add(tone(0.1, db(-6)))
	if got := out.String(); got != " █ █" {
		t.Errorf("got %q, want %q", got, " █ █")
	}
}

func TestTraceMarksClipping(t *testing.T) {
	var out strings.Builder
	tr := newTrace(&out)
	tr.add(sine(-80))
	loud := tone(0.1, db(-6))
	tr.add(loud)
	loud[500] = -32768
	tr.add(loud)
	loud[500] = 32767
	tr.add(loud)
	if got := out.String(); got != " █╋╋" {
		t.Errorf("got %q, want %q", got, " █╋╋")
	}
}

// sine is a 100 Hz wave at level dBFS.
func sine(level float64) []int16 {
	s := make([]int16, frame)
	for i := range s {
		s[i] = int16(db(level) * math.Sqrt2 * 32767 * math.Sin(2*math.Pi*float64(i)/160))
	}
	return s
}

// Steady background noise draws blank once the floor has found it, and a
// whisper above it draws the lowest height though it is below the scale.
func TestTraceBlanksTheBackground(t *testing.T) {
	var out strings.Builder
	tr := newTrace(&out)
	for range noiseWindow {
		tr.add(sine(-60))
	}
	out.Reset()
	tr.add(sine(-60))
	tr.add(sine(-52))
	if got := out.String(); got != " ▁" {
		t.Errorf("got %q, want %q", got, " ▁")
	}
}

// A recording starts in the room, so its first block is the floor.
func TestTraceTakesTheFirstBlockAsTheRoom(t *testing.T) {
	var out strings.Builder
	tr := newTrace(&out)
	tr.add(sine(-50))
	tr.add(tone(0.1, db(-6)))
	if got := out.String(); got != " █" {
		t.Errorf("got %q, want %q", got, " █")
	}
}

func TestBlockLevel(t *testing.T) {
	if got := blockLevel(tone(0.1, 0.1)); math.Abs(got-(-20)) > 0.01 {
		t.Errorf("blockLevel of a square wave at a tenth of full scale = %.2f, want -20", got)
	}
	if got := blockLevel(tone(0.1, 0)); !math.IsInf(got, -1) {
		t.Errorf("blockLevel of zeros = %v, want -Inf", got)
	}
}

// The floor drops to a quieter block at once.
func TestNoiseFloorFallsToAQuietBlock(t *testing.T) {
	var out strings.Builder
	tr := newTrace(&out)
	tr.add(sine(-70))
	if got := tr.noiseFloor(); math.Abs(got-(-70)) > 0.5 {
		t.Errorf("floor = %.1f, want -70", got)
	}
}

// Speech keeps dropping back to the room between words, which holds the floor
// down, so the speech keeps showing.
func TestNoiseFloorStaysUnderSpeech(t *testing.T) {
	var out strings.Builder
	tr := newTrace(&out)
	for range 20 {
		tr.add(sine(-70))
		for range 30 {
			tr.add(sine(-30))
		}
	}
	if got := tr.noiseFloor(); got > -60 {
		t.Errorf("floor = %.1f after speech with pauses, want it near the room, -70", got)
	}
}

// A steady sound that never pauses for five seconds is the room.
func TestNoiseFloorRisesToSteadyNoise(t *testing.T) {
	var out strings.Builder
	tr := newTrace(&out)
	tr.add(sine(-70))
	for range noiseWindow {
		tr.add(sine(-50))
	}
	out.Reset()
	tr.add(sine(-50))
	if got := out.String(); got != " " {
		t.Errorf("steady noise after five seconds drew %q, want blank", got)
	}
}

// A device starting up delivers zeros, which are not the room.
func TestNoiseFloorIgnoresDigitalSilence(t *testing.T) {
	var out strings.Builder
	tr := newTrace(&out)
	for range noiseWindow {
		tr.add(tone(0.1, 0))
	}
	tr.add(sine(-40))
	if got := tr.noiseFloor(); math.Abs(got-(-40)) > 0.5 {
		t.Errorf("floor = %.1f after zeros and the room, want -40", got)
	}
}

// A loud room is blank from its first block.
func TestTraceBlanksALoudRoomAtOnce(t *testing.T) {
	var out strings.Builder
	tr := newTrace(&out)
	for range 10 {
		tr.add(sine(-38))
	}
	if got := out.String(); got != "          " {
		t.Errorf("got %q, want blank", got)
	}
}
