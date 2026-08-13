// Transcribe runs the Moonshine pipeline on WAV files, for repeatable offline
// testing of audio preprocessing without re-recording. Pass -raw to skip
// normalization and see how the model handles the untouched capture.
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/christian-oudard/diktat/internal/asr"
	"github.com/christian-oudard/diktat/internal/audio"
	"github.com/christian-oudard/diktat/internal/models"
	"github.com/christian-oudard/diktat/internal/wav"
)

func runTranscribe(args []string) {
	fs := flag.NewFlagSet("transcribe", flag.ExitOnError)
	raw := fs.Bool("raw", false, "skip normalization")
	name := fs.String("model", models.Default, "model to transcribe with")
	fs.Parse(args)

	modelPath := models.Resolve(*name)
	if err := models.Check(modelPath); err != nil {
		log.Fatalf("%s is not downloaded. Get it with:\n  diktat model download %s", *name, *name)
	}
	model, err := asr.Load(modelPath)
	if err != nil {
		log.Fatalf("load model: %v", err)
	}
	defer model.Close()
	fmt.Println(model.Arch())

	for _, path := range fs.Args() {
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
		t0 := time.Now()
		text, err := model.Transcribe(samples)
		if err != nil {
			log.Printf("%s: transcribe: %v", path, err)
			continue
		}
		// The first file pays for one-off setup, such as compiling the GPU
		// shaders, so compare later ones when timing a backend.
		fmt.Printf("%-24s %5.1fs  peak %.3f  rms %.4f  gain %4.1fx  %6s  ->  %q\n",
			path, float64(len(samples))/float64(audio.SampleRate), peak, rms, gain,
			time.Since(t0).Round(time.Millisecond), text)
	}
}
