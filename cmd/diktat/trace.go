package main

import (
	"io"
	"math"
	"slices"
	"strings"

	"github.com/christian-oudard/diktat/internal/audio"
)

// The level range of the trace in dBFS: blank at the floor, full height at
// the top. Set from labelled takes into a laptop microphone and a headset,
// where it draws quiet speech one or two eighths high, medium three to five
// and loud six to eight, near enough on both.
const (
	traceFloor = -45.0
	traceTop   = -5.0
)

// aboveNoise is how far above the noise floor a block has to be to draw at
// all, in dB: the steady background moves about 4 dB from block to block.
const aboveNoise = 6.0

// noiseWindow is how many blocks back the noise floor looks: five seconds,
// long enough that speech has dropped back to the room between words at
// least once, short enough to take in a fan that starts.
const noiseWindow = 50

// digitalSilence is the level below which a block is a device delivering
// zeros rather than a room, which a headset's gate holds near -95 dBFS.
const digitalSilence = -105.0

// frame is the span drawn as one character.
const frame = audio.SampleRate / 10

var heights = []rune(" ▁▂▃▄▅▆▇█")

// clipped is drawn for a block with a sample at full scale, where the audio
// was cut off rather than recorded.
const clipped = '╋'

// height maps a level in dBFS to an index into heights.
func height(level float64) int {
	h := float64(len(heights) - 1)
	x := (level - traceFloor) / (traceTop - traceFloor) * h
	return int(math.Ceil(math.Max(0, math.Min(h, x))))
}

// blockLevel is the RMS of a block in dBFS, -Inf for zeros.
func blockLevel(b []int16) float64 {
	var energy float64
	for _, s := range b {
		v := float64(s) / 32768
		energy += v * v
	}
	return 10 * math.Log10(energy/float64(len(b)))
}

// trace writes one block per 100 ms of audio as it arrives, and leaves the
// wrapping to the terminal.
type trace struct {
	out     io.Writer
	recent  []float64 // levels of the last noiseWindow blocks
	pending []int16   // samples short of a whole frame
}

func newTrace(out io.Writer) *trace {
	return &trace{out: out}
}

// noiseFloor is the quietest recent block level. Speech keeps falling back to
// the room between words, and steady noise does not move, so the minimum finds
// the room without knowing when anybody spoke. A recording starts in the room,
// so its first block is the floor; before any block it is +Inf.
func (t *trace) noiseFloor() float64 {
	if len(t.recent) == 0 {
		return math.Inf(1)
	}
	return slices.Min(t.recent)
}

func (t *trace) add(samples []int16) {
	t.pending = append(t.pending, samples...)
	var b strings.Builder
	i := 0
	for ; i+frame <= len(t.pending); i += frame {
		f := t.pending[i : i+frame]
		level := blockLevel(f)
		if level >= digitalSilence {
			t.recent = append(t.recent, level)
			if len(t.recent) > noiseWindow {
				t.recent = t.recent[1:]
			}
		}
		if slices.ContainsFunc(f, func(s int16) bool {
			return s == math.MaxInt16 || s == math.MinInt16
		}) {
			b.WriteRune(clipped)
			continue
		}
		h := 0
		if level >= t.noiseFloor()+aboveNoise {
			h = max(1, height(level))
		}
		b.WriteRune(heights[h])
	}
	t.pending = append(t.pending[:0], t.pending[i:]...)
	io.WriteString(t.out, b.String())
}
