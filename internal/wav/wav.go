package wav

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

// SampleRate is the rate everything in diktat speaks: 16 kHz mono.
const SampleRate = 16000

// ReadWAV reads an int16-PCM or float32 WAV file and returns its samples as
// mono float32 in [-1, 1] along with the file's sample rate. Stereo is
// downmixed by averaging channels.
func ReadWAV(path string) ([]float32, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	samples, rate, err := Decode(data)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", path, err)
	}
	return samples, rate, nil
}

// Decode reads a WAV that is already in memory, which is how the warmup takes
// what a synthesiser wrote to its stdout.
func Decode(data []byte) ([]float32, int, error) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, fmt.Errorf("not a WAV file")
	}

	var audioFmt, channels, bits uint16
	var sampleRate uint32
	var pcm []byte
	for off := 12; off+8 <= len(data); {
		id := string(data[off : off+4])
		sz := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		body := data[off+8:]
		if sz > len(body) {
			sz = len(body)
		}
		switch id {
		case "fmt ":
			audioFmt = binary.LittleEndian.Uint16(body[0:2])
			channels = binary.LittleEndian.Uint16(body[2:4])
			sampleRate = binary.LittleEndian.Uint32(body[4:8])
			bits = binary.LittleEndian.Uint16(body[14:16])
		case "data":
			pcm = body[:sz]
		}
		off += 8 + sz + (sz & 1) // chunks are word-aligned
	}
	if pcm == nil {
		return nil, 0, fmt.Errorf("no data chunk")
	}

	ch := int(channels)
	if ch < 1 {
		ch = 1
	}
	var mono []float32
	switch {
	case audioFmt == 1 && bits == 16:
		frames := len(pcm) / 2 / ch
		mono = make([]float32, frames)
		for i := 0; i < frames; i++ {
			var sum float32
			for c := 0; c < ch; c++ {
				s := int16(binary.LittleEndian.Uint16(pcm[2*(i*ch+c):]))
				sum += float32(s) / 32768
			}
			mono[i] = sum / float32(ch)
		}
	case audioFmt == 3 && bits == 32:
		frames := len(pcm) / 4 / ch
		mono = make([]float32, frames)
		for i := 0; i < frames; i++ {
			var sum float32
			for c := 0; c < ch; c++ {
				sum += math.Float32frombits(binary.LittleEndian.Uint32(pcm[4*(i*ch+c):]))
			}
			mono[i] = sum / float32(ch)
		}
	default:
		return nil, 0, fmt.Errorf("unsupported format %d at %d-bit", audioFmt, bits)
	}
	return mono, int(sampleRate), nil
}

// WriteWAV writes samples as a 16-bit PCM mono WAV at the given sample rate.
// For the offline tools, which hold audio as float32; a capture is already
// 16-bit and goes through WriteInt16 without a conversion.
func WriteWAV(path string, samples []float32, rate int) error {
	out := make([]int16, len(samples))
	for i, s := range samples {
		v := s * 32767
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		out[i] = int16(v)
	}
	return WriteInt16(path, out, rate)
}

// WriteInt16 writes samples as a 16-bit PCM mono WAV at the given sample rate.
func WriteInt16(path string, samples []int16, rate int) error {
	var b bytes.Buffer
	dataLen := len(samples) * 2
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+dataLen))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&b, binary.LittleEndian, uint16(1)) // mono
	binary.Write(&b, binary.LittleEndian, uint32(rate))
	binary.Write(&b, binary.LittleEndian, uint32(rate*2)) // byte rate
	binary.Write(&b, binary.LittleEndian, uint16(2))      // block align
	binary.Write(&b, binary.LittleEndian, uint16(16))     // bits
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(dataLen))
	for _, s := range samples {
		binary.Write(&b, binary.LittleEndian, s)
	}
	return os.WriteFile(path, b.Bytes(), 0644)
}
