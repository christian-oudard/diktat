package audio

import "math"

// Preprocessing constants, tuned so quiet mic input reaches a level Moonshine
// transcribes instead of returning empty text.
const (
	normTargetRMS = 0.06
	normFloorRMS  = 0.003
	normMaxGain   = 40.0
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

// Normalize scales samples toward normTargetRMS, boosting quiet mic input that
// Moonshine would otherwise transcribe as empty. RMS (not peak) is targeted
// because quiet speech is spiky: a lone transient keeps the peak high while the
// body stays too quiet to transcribe. Captures below normFloorRMS are treated
// as silence and left untouched. Gain is capped and samples clipped to
// [-1, 1]. It returns the applied gain.
func Normalize(samples []float32, rms float64) float64 {
	if rms < normFloorRMS {
		return 1
	}
	gain := normTargetRMS / rms
	if gain > normMaxGain {
		gain = normMaxGain
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
