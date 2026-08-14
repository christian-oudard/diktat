package audio

import "time"

// buckets are the lengths a warmup rehearses, in seconds. The Vulkan backend
// compiles its shaders per graph shape and the shape follows the length of the
// audio, so a model that has only run one length is cold at every other one.
//
// They are not thresholds anyone has to hit. The backend picks its matmul
// variants in bands, so rehearsing inside a band warms the whole band, and
// what these have to be is dense enough that every band is entered once.
// Measured on a cold shader cache, this set left thirteen unrehearsed lengths
// from 1.2s to 28.8s compiling nothing at all on canary and granite, where a
// sparse set of 1 and 30 seconds left moonshine paying 3.9s at 20 seconds and
// granite 3 to 3.6s at 2, 5 and 10.
//
// Rounding utterances up to a bucket instead was tried and reverted. It makes
// the coverage exact rather than dense, but the encoder work it adds is
// charged on every utterance forever: a 3.2s utterance rounded to 5s cost
// granite 264ms against 80ms, and parakeet 46ms against 23ms. The compiles it
// avoids are paid once per shape.
var buckets = []int{1, 2, 3, 5, 7, 10, 15, 20, 25, 30}

// Warm is the lengths in seconds to rehearse for a model that accepts at most
// max, 0 for no limit. Nothing in the menu declares less than the last bucket.
//
// Past the last bucket nothing is rehearsed, and that is deliberate: dictation
// that long is rare, the audio must not be cut into rehearsed pieces because
// the models window it better themselves, and an unrehearsed shape costs
// seconds only the first time it is met, since the driver keeps compiled
// shaders on disk.
func Warm(max time.Duration) []int {
	for i, secs := range buckets {
		if max > 0 && time.Duration(secs)*time.Second > max {
			return buckets[:i]
		}
	}
	return buckets
}

// Pad lengthens a very short capture with silence to the shortest rehearsed
// length. Below that the graph is a different shape again, and a mis-tap of
// the toggle key or a one word answer would meet it: at 0.4s, canary compiled
// six shaders and took 2.4s where a second of audio takes 20ms.
//
// Nothing longer is padded. Trailing silence does not change what comes back,
// measured across five families, but the encoder work does cost, so it is
// worth adding only where the alternative is a compile.
func Pad(run []float32) []float32 {
	floor := buckets[0] * SampleRate
	if len(run) >= floor {
		return run
	}
	padded := make([]float32, floor)
	copy(padded, run)
	return padded
}
