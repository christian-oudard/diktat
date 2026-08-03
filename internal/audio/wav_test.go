package audio

import (
	"math"
	"path/filepath"
	"testing"
)

func TestWriteReadWAVRoundTrip(t *testing.T) {
	want := sine(0.5, SampleRate/2)
	path := filepath.Join(t.TempDir(), "rt.wav")
	if err := WriteWAV(path, want, SampleRate); err != nil {
		t.Fatalf("WriteWAV: %v", err)
	}
	got, rate, err := ReadWAV(path)
	if err != nil {
		t.Fatalf("ReadWAV: %v", err)
	}
	if rate != SampleRate {
		t.Fatalf("rate = %d, want %d", rate, SampleRate)
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
