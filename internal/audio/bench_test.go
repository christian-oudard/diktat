package audio

import "testing"

// Every sample is converted on its way to the model, so this is on the path
// between the key release and the text appearing.
func BenchmarkFloats20s(b *testing.B) {
	buf := sine(0.02, 20*SampleRate)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Floats(buf)
	}
}
