// Package audio captures 16kHz mono float32 audio via miniaudio (malgo).
package audio

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/gen2brain/malgo"

	"github.com/christian-oudard/diktat/internal/wav"
)

// SampleRate is 16 kHz mono, what moonshine and whisper both expect.
const SampleRate = wav.SampleRate

// RunawayGuard bounds a single capture. It is not a policy about how long
// anyone may speak: how long an utterance may usefully be is the model's
// answer, and the daemon asks it. This is the backstop for a device that
// reports one frame rate and delivers another, where the buffer would
// otherwise grow until the machine noticed. An hour of 16 kHz mono float32 is
// 230 MB, which is survivable and far past any dictation. Samples past it are
// dropped.
const RunawayGuard = time.Hour

// maxSamples is that guard in samples. A var rather than a const so a test
// can shrink it: filling an hour-sized buffer for real would allocate 230 MB
// to prove a bounds check.
var maxSamples = SampleRate * int(RunawayGuard/time.Second)

// Recorder owns a miniaudio context and capture device. Start begins
// accumulating samples; Stop returns and clears them.
type Recorder struct {
	ctx    *malgo.AllocatedContext
	device *malgo.Device

	mu     sync.Mutex
	active bool
	buf    []int16
	level  float64
}

// NewRecorder initializes the miniaudio context and a 16kHz mono 16-bit
// capture device. Recording is gated by Start; the device exists but does
// not produce samples until then.
func NewRecorder() (*Recorder, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("init malgo context: %w", err)
	}

	r := &Recorder{ctx: ctx}

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatS16
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
			samples := make([]int16, frameCount)
			var peak int16
			for i := range samples {
				s := int16(binary.LittleEndian.Uint16(in[2*i:]))
				samples[i] = s
				a := s
				if a < 0 {
					a = -a
				}
				if a > peak {
					peak = a
				}
			}
			r.appendSamples(samples)
			r.level = float64(peak) / full
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

// appendSamples accumulates up to maxSamples, dropping the rest. Callers hold
// r.mu.
func (r *Recorder) appendSamples(samples []int16) {
	room := maxSamples - len(r.buf)
	if room <= 0 {
		return
	}
	if room < len(samples) {
		samples = samples[:room]
	}
	r.buf = append(r.buf, samples...)
}

// Start arms the recorder. Subsequent device callbacks accumulate into buf.
func (r *Recorder) Start() {
	r.mu.Lock()
	r.buf = r.buf[:0]
	r.active = true
	r.mu.Unlock()
}

// Stop disarms the recorder and returns the accumulated samples.
func (r *Recorder) Stop() []int16 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active = false
	out := r.buf
	r.buf = nil
	return out
}

// Level returns the peak amplitude of the most recent capture callback, for a
// live meter while recording.
func (r *Recorder) Level() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.level
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
