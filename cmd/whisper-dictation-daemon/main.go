package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/christian-oudard/whisper_dictation/internal/asr"
)

func main() {
	wavPath := flag.String("wav", "", "transcribe this WAV and exit (one-shot test)")
	flag.Parse()

	modelDir := os.Getenv("MOONSHINE_MODEL_DIR")
	ortLib := os.Getenv("ONNXRUNTIME_LIB")
	if modelDir == "" || ortLib == "" {
		log.Fatal("MOONSHINE_MODEL_DIR and ONNXRUNTIME_LIB must be set")
	}

	model, err := asr.LoadModel(modelDir, ortLib)
	if err != nil {
		log.Fatalf("load model: %v", err)
	}
	defer model.Close()

	if *wavPath != "" {
		audio, err := readWav16kMono(*wavPath)
		if err != nil {
			log.Fatalf("read wav: %v", err)
		}
		text, err := model.Transcribe(audio)
		if err != nil {
			log.Fatalf("transcribe: %v", err)
		}
		fmt.Println(text)
		return
	}

	log.Println("daemon TODO: signal loop, audio capture")
}

func readWav16kMono(path string) ([]float32, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	hdr := make([]byte, 12)
	if _, err := io.ReadFull(f, hdr); err != nil {
		return nil, err
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" {
		return nil, fmt.Errorf("not a WAV file")
	}
	var sampleRate uint32
	var channels, bits uint16
	var pcm []byte
	for {
		var chunkID [4]byte
		var chunkSize uint32
		if _, err := io.ReadFull(f, chunkID[:]); err != nil {
			return nil, err
		}
		if err := binary.Read(f, binary.LittleEndian, &chunkSize); err != nil {
			return nil, err
		}
		body := make([]byte, chunkSize)
		if _, err := io.ReadFull(f, body); err != nil {
			return nil, err
		}
		switch string(chunkID[:]) {
		case "fmt ":
			channels = binary.LittleEndian.Uint16(body[2:4])
			sampleRate = binary.LittleEndian.Uint32(body[4:8])
			bits = binary.LittleEndian.Uint16(body[14:16])
		case "data":
			pcm = body
		}
		if pcm != nil && sampleRate != 0 {
			break
		}
	}
	if channels != 1 || sampleRate != 16000 || bits != 16 {
		return nil, fmt.Errorf("need 16-bit PCM mono 16kHz, got %dch %dHz %dbit",
			channels, sampleRate, bits)
	}
	out := make([]float32, len(pcm)/2)
	for i := range out {
		s := int16(binary.LittleEndian.Uint16(pcm[2*i : 2*i+2]))
		out[i] = float32(s) / 32768.0
	}
	return out, nil
}
