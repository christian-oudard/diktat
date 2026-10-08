package main

import (
	"io"
	"math"
	"slices"
	"strings"

	"github.com/christian-oudard/diktat/internal/audio"
)

// heightStep is the score difference between neighbouring heights.
const heightStep = 5.0

// brightnessWeight is how much a block's brightness counts in its score. A
// voice pushed harder gets brighter as well as louder, and keeps getting
// brighter where a microphone holds its level near clipping.
const brightnessWeight = 1.5

// speechWindow is how many drawn blocks the speaker's typical score is taken
// over: thirty seconds of speech, so a louder or quieter stretch moves the
// scale slowly enough to show as louder or quieter first.
const speechWindow = 300

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

// height maps a score to an index into heights, with the typical score in the
// middle of the fourth.
func height(score, typical float64) int {
	x := (score-typical)/heightStep + 3.5
	return int(math.Ceil(math.Max(1, math.Min(float64(len(heights)-1), x))))
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

// effort scores how hard a block was spoken: its level plus brightnessWeight
// times its brightness, the energy left after the pre-emphasis filter
// x[n] - 0.95 x[n-1] relative to the block's whole energy, in dB. Neither a
// microphone's gain nor its tone matters, since every score is read against
// the speaker's typical one.
func effort(b []int16, level float64) float64 {
	var energy, emphasised float64
	for i, s := range b {
		v := float64(s)
		energy += v * v
		if i > 0 {
			d := v - 0.95*float64(b[i-1])
			emphasised += d * d
		}
	}
	brightness := 10 * math.Log10(emphasised/float64(len(b)-1)/(energy/float64(len(b))))
	return level + brightnessWeight*brightness
}

// trace writes one block per 100 ms of audio as it arrives, and leaves the
// wrapping to the terminal.
type trace struct {
	out     io.Writer
	recent  []float64 // levels of the last noiseWindow blocks
	speech  []float64 // scores of the last speechWindow blocks drawn
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

// typical is the speaker's usual score: the 75th percentile of the blocks
// drawn recently, so the scale follows the voice rather than the microphone,
// and the quieter pauses inside speech do not drag it down.
func (t *trace) typical() float64 {
	s := slices.Sorted(slices.Values(t.speech))
	return s[len(s)*3/4]
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
		if level < t.noiseFloor()+aboveNoise {
			b.WriteRune(heights[0])
			continue
		}
		score := effort(f, level)
		t.speech = append(t.speech, score)
		if len(t.speech) > speechWindow {
			t.speech = t.speech[1:]
		}
		b.WriteRune(heights[height(score, t.typical())])
	}
	t.pending = append(t.pending[:0], t.pending[i:]...)
	io.WriteString(t.out, b.String())
}
