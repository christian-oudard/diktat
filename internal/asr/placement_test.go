package asr

import (
	"testing"

	transcribe "github.com/handy-computer/transcribe.cpp/bindings/go"
)

// A discrete card beats the integrated one beside it, and an integrated GPU
// beats the CPU: measured on an Iris Xe, every model on the menu ran 5 to 23
// times faster there than on the CPU of the same laptop.
func TestBestGPU(t *testing.T) {
	cpu := transcribe.Device{Name: "CPU", Type: transcribe.DeviceCPU}
	igpu := transcribe.Device{Name: "Vulkan0", Type: transcribe.DeviceIGPU}
	gpu := transcribe.Device{Name: "Vulkan1", Type: transcribe.DeviceGPU}
	for _, c := range []struct {
		what    string
		devices []transcribe.Device
		want    int
	}{
		{"discrete after integrated", []transcribe.Device{igpu, gpu, cpu}, 1},
		{"integrated only", []transcribe.Device{igpu, cpu}, 0},
		{"no GPU", []transcribe.Device{cpu}, -1},
	} {
		if got := bestGPU(c.devices); got != c.want {
			t.Errorf("%s: got %d, want %d", c.what, got, c.want)
		}
	}
}
