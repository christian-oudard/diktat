// Package audio captures 16kHz mono float32 audio via miniaudio (malgo).
package audio

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"

	"github.com/gen2brain/malgo"
)

const SampleRate = 16000

// Recorder owns a miniaudio context and capture device. Start begins
// accumulating samples; Stop returns and clears them.
type Recorder struct {
	ctx    *malgo.AllocatedContext
	device *malgo.Device

	mu     sync.Mutex
	active bool
	buf    []float32
}

// NewRecorder initializes the miniaudio context and a 16kHz mono float32
// capture device. Recording is gated by Start; the device exists but does
// not produce samples until then.
func NewRecorder() (*Recorder, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("init malgo context: %w", err)
	}

	r := &Recorder{ctx: ctx}

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatF32
	cfg.Capture.Channels = 1
	cfg.SampleRate = SampleRate
	cfg.Alsa.NoMMap = 1

	callbacks := malgo.DeviceCallbacks{
		Data: func(_, in []byte, frameCount uint32) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if !r.active {
				return
			}
			samples := make([]float32, frameCount)
			for i := range samples {
				bits := binary.LittleEndian.Uint32(in[4*i:])
				samples[i] = math.Float32frombits(bits)
			}
			r.buf = append(r.buf, samples...)
		},
	}

	dev, err := malgo.InitDevice(ctx.Context, cfg, callbacks)
	if err != nil {
		_ = ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("init capture device: %w", err)
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		_ = ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("start capture device: %w", err)
	}
	r.device = dev
	return r, nil
}

// Start arms the recorder. Subsequent device callbacks accumulate into buf.
func (r *Recorder) Start() {
	r.mu.Lock()
	r.buf = r.buf[:0]
	r.active = true
	r.mu.Unlock()
}

// Stop disarms the recorder and returns the accumulated samples.
func (r *Recorder) Stop() []float32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active = false
	out := r.buf
	r.buf = nil
	return out
}

// Close stops the device and releases the context.
func (r *Recorder) Close() {
	if r.device != nil {
		r.device.Uninit()
	}
	if r.ctx != nil {
		_ = r.ctx.Uninit()
		r.ctx.Free()
	}
}
