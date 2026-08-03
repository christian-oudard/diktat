package audio

import "testing"

func BenchmarkNormalize20s(b *testing.B) {
	base := sine(0.02, 20*SampleRate) // 20 seconds of quiet audio
	buf := make([]float32, len(base))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(buf, base)
		Normalize(buf)
	}
}
