package audio

// Resample converts between sample rates by linear interpolation. It has one
// caller: espeak renders the warmup at its voice's 22050 Hz and the pipeline
// wants 16000, and nothing else in diktat changes rate.
//
// Linear interpolation aliases, which would be the wrong choice for audio a
// model has to transcribe accurately. The warmup is not transcribed for its
// text: it exists to make the backend compile the shapes a real utterance
// needs, and those follow the length of the audio and the number of tokens it
// provokes, neither of which cares about the noise floor above 8 kHz.
func Resample(in []float32, from, to int) []float32 {
	if from == to || len(in) == 0 {
		return in
	}
	out := make([]float32, len(in)*to/from)
	ratio := float64(from) / float64(to)
	for i := range out {
		pos := float64(i) * ratio
		j := int(pos)
		if j+1 >= len(in) {
			out[i] = in[len(in)-1]
			continue
		}
		frac := float32(pos - float64(j))
		out[i] = in[j]*(1-frac) + in[j+1]*frac
	}
	return out
}
