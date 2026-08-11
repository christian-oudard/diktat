package wav

import (
	"math"
	"path/filepath"
	"testing"
)

func sine(amp float32, n int) []float32 {
	s := make([]float32, n)
	for i := range s {
		s[i] = amp * float32(math.Sin(2*math.Pi*220*float64(i)/SampleRate))
	}
	return s
}

func TestWriteReadWAVRoundTrip(t *testing.T) {
	want := sine(0.5, 16000/2)
	path := filepath.Join(t.TempDir(), "rt.wav")
	if err := WriteWAV(path, want, 16000); err != nil {
		t.Fatalf("WriteWAV: %v", err)
	}
	got, rate, err := ReadWAV(path)
	if err != nil {
		t.Fatalf("ReadWAV: %v", err)
	}
	if rate != 16000 {
		t.Fatalf("rate = %d, want %d", rate, 16000)
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if math.Abs(float64(got[i]-want[i])) > 1e-4 { // 16-bit quantization
			t.Fatalf("sample %d = %f, want %f", i, got[i], want[i])
		}
	}
}
