// transcribe runs the daemon's own pipeline over WAV files, so a model or a
// preprocessing change can be measured against fixed audio instead of against
// a fresh utterance every time. Pass -raw to skip normalization and hear what
// the model makes of the untouched capture.
//
// Deliberately not a diktat subcommand: nothing here is part of dictating, so
// it is not worth a place in the shipped binary. The flake builds only
// cmd/diktat, which leaves this to `go run ./cmd/transcribe` inside the
// devShell. It lives in Go rather than in a script because it has to run the
// real pipeline, and a script would have to reimplement it.
//
//	go run ./cmd/transcribe [-raw] [-model <name>] file.wav...
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/christian-oudard/diktat/internal/asr"
	"github.com/christian-oudard/diktat/internal/audio"
	"github.com/christian-oudard/diktat/internal/models"
	"github.com/christian-oudard/diktat/internal/wav"
)

func main() {
	fs := flag.NewFlagSet("transcribe", flag.ExitOnError)
	raw := fs.Bool("raw", false, "skip normalization")
	name := fs.String("model", models.Default, "model to transcribe with")
	fs.Parse(os.Args[1:])

	modelPath := models.Resolve(*name)
	if err := models.Check(modelPath); err != nil {
		log.Fatalf("%s is not downloaded. Get it with:\n  diktat model %s", *name, *name)
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
