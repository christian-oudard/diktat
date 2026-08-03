package audio

import "math"

// Normalization targets a fixed peak level so Moonshine sees a consistent
// amplitude regardless of the mic. Moonshine's empty-vs-text decision is
// governed by signal quality, not level: clean speech transcribes across a
// wide amplitude range, so the goal is only to lift quiet input to a normal
// level, never to fix noise (which gain cannot do).
const (
	normTargetPeak = 0.9   // scale the reference level to this
	normPercentile = 0.999 // reference level ignores the loudest 0.1% (clicks, plosives)
	normFloor      = 0.005 // below this reference level, treat as silence and skip
	normMaxGain    = 100.0
)

// Levels returns the peak absolute amplitude and RMS of the samples.
func Levels(samples []float32) (peak, rms float64) {
	var sumSq float64
	for _, s := range samples {
		a := math.Abs(float64(s))
		if a > peak {
			peak = a
		}
		sumSq += float64(s) * float64(s)
	}
	if len(samples) > 0 {
		rms = math.Sqrt(sumSq / float64(len(samples)))
	}
	return peak, rms
}

// Normalize scales samples so a high percentile of their amplitude reaches
// normTargetPeak, boosting quiet mic input that Moonshine would otherwise
// transcribe as empty. Using a percentile rather than the true peak ignores
// rare loud transients (clicks, plosives) that would otherwise suppress the
// gain, and using peak rather than RMS is unaffected by how much silence a
// clip contains. Input below normFloor is treated as silence and left alone;
// input already above the target is left alone. The rare samples above the
// target are clipped. It returns the applied gain.
func Normalize(samples []float32) float64 {
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
	for i := range samples {
		v := float64(samples[i]) * gain
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		samples[i] = float32(v)
	}
	return gain
}

// percentileAbs returns the p-quantile (0..1) of the absolute sample values.
// It bins magnitudes into a fixed histogram for an O(n) single pass, rather
// than sorting; the returned bin upper edge slightly overestimates the level,
// which biases the gain down and so avoids extra clipping. Bin width
// (1/bins) bounds the error, negligible against the fixed normalization target.
func percentileAbs(samples []float32, p float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	const bins = 4096
	var hist [bins]int
	for _, s := range samples {
		idx := int(math.Abs(float64(s)) * bins)
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
