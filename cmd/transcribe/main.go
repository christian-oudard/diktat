// transcribe runs the daemon's own pipeline over WAV files, so a model or a
// pipeline change can be measured against fixed audio instead of against a
// fresh utterance every time.
//
// Deliberately not a diktat subcommand: nothing here is part of dictating, so
// it is not worth a place in the shipped binary. The flake builds only
// cmd/diktat, which leaves this to `go run ./cmd/transcribe` inside the
// devShell. It lives in Go rather than in a script because it has to run the
// real pipeline, and a script would have to reimplement it.
//
//	go run ./cmd/transcribe [-model <name>] [-live] recording...
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	transcribe "github.com/handy-computer/transcribe.cpp/bindings/go"

	"github.com/christian-oudard/diktat/internal/asr"
	"github.com/christian-oudard/diktat/internal/audio"
	"github.com/christian-oudard/diktat/internal/human"
	"github.com/christian-oudard/diktat/internal/models"
	"github.com/christian-oudard/diktat/internal/phrase"
	"github.com/christian-oudard/diktat/internal/warmup"
	"github.com/christian-oudard/diktat/internal/wav"
)

// transcribeWith runs the daemon's own call, or the same with punctuation
// asked for. The default is what the daemon does, since that is what this
// tool exists to measure; the flag is here because the family defaults differ
// and the difference is not cosmetic. parakeet-tdt-0.6b-v2 punctuates
// unasked and parakeet-tdt-1.1b returns none, which decides whether either is
// usable in a document, and neither advertises the feature.
func transcribeWith(m *asr.Model, samples []float32, pnc bool) (string, error) {
	if !pnc {
		return m.Transcribe(context.Background(), samples)
	}
	res, err := m.Run(context.Background(), samples, &transcribe.RunOptions{PNC: transcribe.ModeOn})
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

func main() {
	fs := flag.NewFlagSet("transcribe", flag.ExitOnError)
	pnc := fs.Bool("pnc", false, "ask for punctuation and casing rather than taking the family default")
	limitFlag := fs.Duration("limit", 0, "cut audio at this length instead of what the model can take")
	name := fs.String("model", models.Default, "model to transcribe with")
	liveFlag := fs.Bool("live", false, "replay each file as if dictated, transcribing phrases as the pauses arrive")
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
	// Warmed like the daemon warms, which is not only about the first file's
	// timings. A model that has run nothing does not know how much audio it
	// can take in one graph and falls back to a 30 second floor, and cutting
	// a clip there changes what comes back: on this passage it cost
	// canary-180m-flash 19 points of word error rate and saved parakeet 4.
	if _, err := warmup.Run(context.Background(), model); err != nil {
		log.Printf("warmup: %v", err)
	}
	// The limit is part of what a model is, not a detail: it says how much of
	// an utterance this one takes in a single graph before the audio has to be
	// cut, which on a big model on a small card is under a minute.
	fmt.Printf("%s, %s resident, good for %s of audio (%s)\n", model.Arch(),
		human.Bytes(model.Bytes()), model.AudioLimit().Round(time.Second),
		model.LoadTimings())

	var det *phrase.Detector
	if *liveFlag {
		if det, err = phrase.LoadDetector(); err != nil {
			log.Fatalf("voice activity detector: %v", err)
		}
		defer det.Close()
	}

	for _, path := range fs.Args() {
		stored, err := load(path)
		if err != nil {
			log.Printf("%v", err)
			continue
		}
		if det != nil {
			live(model, det, path, stored)
			continue
		}
		peak, rms := audio.Levels(stored)
		t0 := time.Now()
		// Cut and padded like the daemon does it, so a file measures what an
		// utterance of that length would cost, down to the graph shape.
		limit := model.AudioLimit()
		if *limitFlag != 0 {
			limit = *limitFlag
		}
		var parts []string
		fail := false
		for _, chunk := range audio.Chunk(stored, int(limit.Seconds())*audio.SampleRate) {
			part, err := transcribeWith(model, audio.Pad(audio.Floats(chunk)), *pnc)
			if err != nil {
				log.Printf("%s: transcribe: %v", path, err)
				fail = true
				break
			}
			parts = append(parts, part)
		}
		if fail {
			continue
		}
		text := strings.Join(parts, " ")
		// The first file pays for one-off setup, such as compiling the GPU
		// shaders, so compare later ones when timing a backend.
		fmt.Printf("%-24s %5.1fs  peak %.3f  rms %.4f  %6s  ->  %q\n",
			path, float64(len(stored))/float64(audio.SampleRate), peak, rms,
			time.Since(t0).Round(time.Millisecond), text)
	}
}

// load reads a recording into the form a capture is held in, so a file
// follows exactly the path a recording does.
func load(path string) ([]int16, error) {
	samples, rate, err := wav.Read(path)
	if err != nil {
		return nil, err
	}
	if rate != audio.SampleRate {
		return nil, fmt.Errorf("%s: sample rate %d != %d", path, rate, audio.SampleRate)
	}
	return audio.Ints(samples), nil
}

// live replays a file the way the daemon hears a dictation: a quarter second
// at a time, transcribing up to each pause the detector finds whenever the
// model is free, and the rest once the file ends. The wait is from the end of
// the file to the text, which is what the speaker sits through.
func live(model *asr.Model, det *phrase.Detector, path string, stored []int16) {
	ctx := context.Background()
	step := audio.SampleRate / 4
	var parts []string
	cut := 0
	free := time.Duration(0) // when the model is next free, in the file's time
	for at := step; at < len(stored); at += step {
		now := time.Duration(at) * time.Second / audio.SampleRate
		if now < free {
			continue
		}
		t0 := time.Now()
		end, err := det.End(ctx, stored[cut:at])
		if err != nil {
			log.Fatalf("%s: detect: %v", path, err)
		}
		if end > 0 {
			part, err := phrase.Transcribe(ctx, model, stored[cut:cut+end])
			if err != nil {
				log.Fatalf("%s: transcribe: %v", path, err)
			}
			fmt.Printf("  phrase %5.1fs-%5.1fs at %5.1fs took %6s: %q\n",
				float64(cut)/audio.SampleRate, float64(cut+end)/audio.SampleRate, now.Seconds(),
				time.Since(t0).Round(time.Millisecond), part)
			parts = append(parts, part)
			cut += end
		}
		free = now + time.Since(t0)
	}
	clip := time.Duration(len(stored)) * time.Second / audio.SampleRate
	behind := max(free-clip, 0)
	t0 := time.Now()
	tail, err := phrase.Transcribe(ctx, model, stored[cut:])
	if err != nil {
		log.Fatalf("%s: transcribe: %v", path, err)
	}
	if tail != "" {
		parts = append(parts, tail)
	}
	fmt.Printf("%-24s %5.1fs  last %4.1fs  wait %6s  ->  %q\n", path, clip.Seconds(),
		float64(len(stored)-cut)/audio.SampleRate, (behind + time.Since(t0)).Round(time.Millisecond),
		strings.Join(parts, " "))
}
