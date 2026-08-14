package audio

// Chunk splits a capture into pieces the model will accept, cutting where the
// audio is quietest so a seam falls between words rather than through one.
//
// max of 0 means no limit, which is what most families report: they chunk
// internally and would rather see the whole utterance than our idea of where
// it divides. Only a family that carries audio in a decoder context has a
// real ceiling, and then this is what keeps a long dictation from being
// refused outright.
func Chunk(samples []int16, max int) [][]int16 {
	if max <= 0 || len(samples) <= max {
		return [][]int16{samples}
	}
	var out [][]int16
	for len(samples) > max {
		cut := quietest(samples, max)
		out = append(out, samples[:cut])
		samples = samples[cut:]
	}
	return append(out, samples)
}

// seamSearch is how much of the end of a piece to search for a quiet moment,
// as a fraction of the limit. A fifth is enough to reach the gap between two
// sentences without giving up much of the piece.
const seamSearch = 5

// quietest is where to cut a piece of at most max samples: the start of the
// quietest window in the last max/seamSearch samples, or max itself when the
// audio has no lull there, since a cut somewhere beats a piece the model
// refuses.
func quietest(samples []int16, max int) int {
	const window = SampleRate / 10 // 100ms, about the length of a pause
	from := max - max/seamSearch
	if from < window {
		return max
	}
	best, bestEnergy := max, int64(-1)
	// A tenth of the window is fine enough to find a gap and coarse enough to
	// stay cheap on a long capture.
	for i := from; i+window <= max; i += window / 10 {
		var energy int64
		for _, s := range samples[i : i+window] {
			if s < 0 {
				s = -s
			}
			energy += int64(s)
		}
		if bestEnergy < 0 || energy < bestEnergy {
			best, bestEnergy = i+window/2, energy
		}
	}
	return best
}
