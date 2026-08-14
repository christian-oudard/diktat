package audio

import "math"

// Captures are held as signed 16-bit samples, which is what the microphone
// resolves and what the diagnostic wav stores anyway. Float32 doubles the
// memory for no more information, and with recording no longer capped, the
// buffer is as long as someone cares to speak. The model wants float32, so
// Floats converts a piece at a time on the way in.

// Normalization targets a fixed peak level so the model sees a consistent
// amplitude regardless of the mic. The empty-vs-text decision is governed by
// signal quality, not level: clean speech transcribes across a wide amplitude
// range, so the goal is only to lift quiet input to a normal level, never to
// fix noise (which gain cannot do).
const (
	normTargetPeak = 0.9   // scale the reference level to this
	normPercentile = 0.999 // reference level ignores the loudest 0.1% (clicks, plosives)
	normFloor      = 0.005 // below this reference level, treat as silence and skip
	normMaxGain    = 100.0
)

// full is the magnitude of a full-scale sample, the divisor between the
// stored form and the [-1, 1] the model works in.
const full = 32768

// Levels returns the peak absolute amplitude and RMS of the samples, both on
// the [0, 1] scale the log and the normalization thresholds use.
func Levels(samples []int16) (peak, rms float64) {
	var sumSq float64
	for _, s := range samples {
		v := float64(s) / full
		if a := math.Abs(v); a > peak {
			peak = a
		}
		sumSq += v * v
	}
	if len(samples) > 0 {
		rms = math.Sqrt(sumSq / float64(len(samples)))
	}
	return peak, rms
}

// IsSilent reports whether a capture holds no speech worth transcribing,
// using the same reference level and floor Gain declines to amplify at.
//
// Worth asking before running a model rather than after: an audio-LLM given
// silence has nothing to transcribe and describes something instead. Voxtral
// answers four seconds of faint noise with a timestamped monologue about a
// talk that never happened, and keeps going until its decode budget stops it.
// The encoder-decoder families are cheaper about it but still emit their
// non-speech markers.
func IsSilent(samples []int16) bool {
	return percentileAbs(samples, normPercentile) < normFloor
}

// Gain is what a capture should be scaled by so that a high percentile of its
// amplitude reaches normTargetPeak, boosting quiet mic input the model would
// otherwise transcribe as empty. Using a percentile rather than the true peak
// ignores rare loud transients (clicks, plosives) that would otherwise
// suppress the gain, and using peak rather than RMS is unaffected by how much
// silence a clip contains. Input below normFloor is treated as silence, and
// input already above the target is left alone; both answer 1.
//
// It is computed over the whole capture and applied to every piece of it, so
// a long recording split into chunks does not change loudness at the seams.
func Gain(samples []int16) float64 {
	level := percentileAbs(samples, normPercentile)
	if level < normFloor {
		return 1
	}
	gain := normTargetPeak / level
	if gain > normMaxGain {
		gain = normMaxGain
	}
	if gain <= 1 {
		return 1
	}
	return gain
}

// Floats converts stored samples to what the model takes, applying gain and
// clipping the rare sample it pushes past full scale.
func Floats(samples []int16, gain float64) []float32 {
	out := make([]float32, len(samples))
	for i, s := range samples {
		v := float64(s) / full * gain
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		out[i] = float32(v)
	}
	return out
}

// Ints is the way back, for the offline tools: they read float32 out of a wav
// and then follow the same path as a capture.
func Ints(samples []float32) []int16 {
	out := make([]int16, len(samples))
	for i, s := range samples {
		v := float64(s) * full
		if v > full-1 {
			v = full - 1
		} else if v < -full {
			v = -full
		}
		out[i] = int16(v)
	}
	return out
}

// percentileAbs returns the p-quantile (0..1) of the absolute sample values.
// It bins magnitudes into a fixed histogram for an O(n) single pass, rather
// than sorting; the returned bin upper edge slightly overestimates the level,
// which biases the gain down and so avoids extra clipping. Bin width
// (1/bins) bounds the error, negligible against the fixed normalization target.
func percentileAbs(samples []int16, p float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	const bins = 4096
	var hist [bins]int
	for _, s := range samples {
		idx := int(math.Abs(float64(s)) / full * bins)
		if idx >= bins {
			idx = bins - 1
		}
		hist[idx]++
	}
	target := int(p * float64(len(samples)))
	cum := 0
	for i := 0; i < bins; i++ {
		if cum += hist[i]; cum > target {
			return float64(i+1) / bins
		}
	}
	return 1
}
