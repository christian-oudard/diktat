// Transcribe runs the Moonshine pipeline on WAV files, for repeatable offline
// testing of audio preprocessing without re-recording. Pass -raw to skip
// normalization and see how the model handles the untouched capture.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/christian-oudard/diktat/internal/asr"
	"github.com/christian-oudard/diktat/internal/audio"
	"github.com/christian-oudard/diktat/internal/wav"
)

func main() {
	raw := flag.Bool("raw", false, "skip normalization")
	flag.Parse()

	modelDir := os.Getenv("MOONSHINE_MODEL_DIR")
	ortLib := os.Getenv("ONNXRUNTIME_LIB")
	if modelDir == "" || ortLib == "" {
		log.Fatal("MOONSHINE_MODEL_DIR and ONNXRUNTIME_LIB must be set")
	}
	// Accepts a whisper .bin as readily as a moonshine directory.
	model, err := asr.Load(modelDir, ortLib)
	if err != nil {
		log.Fatalf("load model: %v", err)
	}
	defer model.Close()

	for _, path := range flag.Args() {
		samples, rate, err := wav.ReadWAV(path)
		if err != nil {
			log.Printf("%v", err)
			continue
		}
		if rate != audio.SampleRate {
			log.Printf("%s: sample rate %d != %d", path, rate, audio.SampleRate)
			continue
		}
		peak, rms := audio.Levels(samples)
		gain := 1.0
		if !*raw {
			gain = audio.Normalize(samples)
		}
		text, err := model.Transcribe(samples)
		if err != nil {
			log.Printf("%s: transcribe: %v", path, err)
			continue
		}
		fmt.Printf("%-24s %5.1fs  peak %.3f  rms %.4f  gain %4.1fx  ->  %q\n",
			path, float64(len(samples))/float64(audio.SampleRate), peak, rms, gain, text)
	}
}
