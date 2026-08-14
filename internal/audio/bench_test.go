package audio

import "testing"

// Gain reads the whole capture to pick one number, and with recording no
// longer capped that read can be over an hour of audio.
func BenchmarkGain20s(b *testing.B) {
	buf := sine(0.02, 20*SampleRate) // 20 seconds of quiet audio
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Gain(buf)
	}
}

// Every sample is converted on its way to the model, so this is on the path
// between the key release and the text appearing.
func BenchmarkFloats20s(b *testing.B) {
	buf := sine(0.02, 20*SampleRate)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Floats(buf, 4)
	}
}
