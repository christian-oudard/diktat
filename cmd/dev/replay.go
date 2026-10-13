package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/christian-oudard/diktat/internal/asr"
	"github.com/christian-oudard/diktat/internal/audio"
	"github.com/christian-oudard/diktat/internal/config"
	"github.com/christian-oudard/diktat/internal/debuglog"
	"github.com/christian-oudard/diktat/internal/flagset"
	"github.com/christian-oudard/diktat/internal/human"
	"github.com/christian-oudard/diktat/internal/models"
	"github.com/christian-oudard/diktat/internal/phrase"
	"github.com/christian-oudard/diktat/internal/warmup"
	"github.com/christian-oudard/diktat/internal/wav"
)

// names collects a flag given more than once.
type names []string

func (n *names) String() string     { return strings.Join(*n, ",") }
func (n *names) Set(v string) error { *n = append(*n, v); return nil }

// recording is one file to replay, and what was typed from it when it was
// dictated, if debug mode kept it.
type recording struct {
	path     string
	samples  []int16
	dictated *debuglog.Entry
}

// result is what one model made of one recording.
type result struct {
	mode string
	wait time.Duration
	text string
	note string // what the mode adds: commit lag, or phrases cut
}

// runReplay replays recordings as if each had just been dictated, so a model
// or a change to the pipeline is measured against fixed audio instead of
// against a fresh utterance every time. See docs/dev.md.
func runReplay(args []string) {
	fs := flagset.New("dev replay")
	var modelNames names
	fs.Var(&modelNames, "model", "model to replay with; give it again to compare several\n(default: the one the daemon starts on)")
	mode := fs.String("mode", "auto", "auto, whole, phrases or stream")
	last := fs.Int("last", 0, "with no recordings named, replay only the newest `N` debug mode kept")
	fs.Parse(args)
	switch *mode {
	case "auto", "whole", "phrases", "stream":
	default:
		log.Fatalf("unknown mode %q: auto, whole, phrases or stream", *mode)
	}
	if len(modelNames) == 0 {
		modelNames = names{config.StartModel()}
	}

	recs, err := recordings(fs.Args(), *last)
	if err != nil {
		log.Fatal(err)
	}
	if len(recs) == 0 {
		log.Fatalf("nothing to replay: name recordings, or keep some with debug mode (docs/debug.md)")
	}

	var det *phrase.Detector
	results := make([][]result, len(recs))
	for _, name := range modelNames {
		model := loadWarm(name)
		m := modeFor(model, *mode)
		if m == "phrases" && det == nil {
			if det, err = phrase.LoadDetector(); err != nil {
				log.Fatalf("voice activity detector: %v", err)
			}
			defer det.Close()
		}
		for i, r := range recs {
			results[i] = append(results[i], replay(model, det, m, r.samples))
		}
		model.Close()
	}

	width := len("dictated")
	for _, name := range modelNames {
		width = max(width, len(name))
	}
	for i, r := range recs {
		peak, rms := audio.Levels(r.samples)
		fmt.Printf("\n%s  %.1fs  peak %.3f  rms %.4f\n", filepath.Base(r.path), seconds(len(r.samples)), peak, rms)
		if e := r.dictated; e != nil {
			wait := "-"
			if e.WaitMS != nil {
				wait = (time.Duration(e.WaitMS.Total) * time.Millisecond).String()
			}
			fmt.Printf("  %-*s %-7s wait %6s  %-24s %q\n", width, "dictated", "", wait, strings.TrimSuffix(e.Model, ".gguf"), e.Text)
		}
		for j, res := range results[i] {
			fmt.Printf("  %-*s %-7s wait %6s  %-24s %q\n", width, modelNames[j], res.mode,
				res.wait.Round(time.Millisecond), res.note, res.text)
		}
	}
}

// recordings reads the files named, or with none the newest last of the ones
// debug mode kept, or all of them when last is 0. A kept one carries what was
// typed from it.
func recordings(paths []string, last int) ([]recording, error) {
	entries, err := debuglog.Read()
	if err != nil {
		return nil, err
	}
	kept := map[string]*debuglog.Entry{}
	for i := range entries {
		kept[filepath.Join(debuglog.Dir(), entries[i].Audio)] = &entries[i]
	}
	if len(paths) == 0 {
		for _, e := range entries {
			path := filepath.Join(debuglog.Dir(), e.Audio)
			if _, err := os.Stat(path); err == nil {
				paths = append(paths, path)
			}
		}
		if last > 0 && last < len(paths) {
			paths = paths[len(paths)-last:]
		}
	}
	var recs []recording
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		samples, err := load(path)
		if err != nil {
			return nil, err
		}
		recs = append(recs, recording{path, samples, kept[abs]})
	}
	return recs, nil
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

// loadWarm loads a model and warms it as the daemon does, which is not only
// about the first recording's wait. A model that has run nothing does not know
// how much audio it can take in one graph and falls back to a 30 second floor,
// and cutting a clip there changes what comes back: on one passage it cost
// canary-180m-flash 19 points of word error rate and saved parakeet 4. A model
// that streams is also given one stream, since a stream's graphs are not the
// ones the warmup runs.
func loadWarm(name string) *asr.Model {
	path := models.Resolve(name)
	if err := models.Check(path); err != nil {
		log.Fatalf("%s is not downloaded. Get it with:\n  diktat model %s", name, name)
	}
	model, err := asr.Load(path)
	if err != nil {
		log.Fatalf("load %s: %v", name, err)
	}
	if _, err := warmup.Run(context.Background(), model); err != nil {
		log.Printf("warmup: %v", err)
	}
	if streams, _ := model.Streams(); streams {
		speech, err := warmup.Speech()
		if err != nil {
			log.Fatalf("warmup: %v", err)
		}
		replayStream(model, audio.Ints(speech))
	}
	fmt.Fprintf(os.Stderr, "%s, %s resident, good for %s of audio (%s)\n", model.Arch(),
		human.Bytes(model.Bytes()), model.AudioLimit().Round(time.Second), model.LoadTimings())
	return model
}

// modeFor resolves auto to what the daemon would do with the model.
func modeFor(model *asr.Model, mode string) string {
	if mode != "auto" {
		return mode
	}
	if streams, _ := model.Streams(); streams {
		return "stream"
	}
	if models.Check(models.Detector.Path()) == nil {
		return "phrases"
	}
	return "whole"
}

func replay(model *asr.Model, det *phrase.Detector, mode string, samples []int16) result {
	var res result
	switch mode {
	case "whole":
		t0 := time.Now()
		text, err := phrase.Transcribe(context.Background(), model, samples)
		if err != nil {
			log.Fatalf("transcribe: %v", err)
		}
		res = result{wait: time.Since(t0), text: text}
	case "phrases":
		res = replayPhrases(model, det, samples)
	case "stream":
		res = replayStream(model, samples)
	}
	res.mode = mode
	return res
}

// step is how much audio arrives between two looks, as in the daemon.
const step = audio.SampleRate / 4

func seconds(samples int) float64 { return float64(samples) / audio.SampleRate }

func at(samples int) time.Duration { return time.Duration(samples) * time.Second / audio.SampleRate }

// replayPhrases cuts a recording at its pauses while it plays, transcribing up
// to each pause the detector finds whenever the model is free, and the rest
// once it ends.
func replayPhrases(model *asr.Model, det *phrase.Detector, samples []int16) result {
	ctx := context.Background()
	var parts []string
	cut := 0
	free := time.Duration(0) // when the model is next free, in the recording's time
	for end := step; end < len(samples); end += step {
		if at(end) < free {
			continue
		}
		t0 := time.Now()
		n, err := det.End(ctx, samples[cut:end])
		if err != nil {
			log.Fatalf("detect: %v", err)
		}
		if n > 0 {
			part, err := phrase.Transcribe(ctx, model, samples[cut:cut+n])
			if err != nil {
				log.Fatalf("transcribe: %v", err)
			}
			if part != "" {
				parts = append(parts, part)
			}
			cut += n
		}
		free = at(end) + time.Since(t0)
	}
	phrases := len(parts)
	behind := max(free-at(len(samples)), 0)
	t0 := time.Now()
	tail, err := phrase.Transcribe(ctx, model, samples[cut:])
	if err != nil {
		log.Fatalf("transcribe: %v", err)
	}
	if tail != "" {
		parts = append(parts, tail)
	}
	return result{wait: behind + time.Since(t0), text: strings.Join(parts, " "),
		note: fmt.Sprintf("phrases %d, last %.1fs", phrases, seconds(len(samples)-cut))}
}

// replayStream feeds a recording to the model's stream a quarter second at a
// time, each piece once it has arrived and the model is done with the one
// before. lag is, for each token, how long after it was spoken the feed came
// that committed it, which is how late words would appear if they were typed
// as they committed.
func replayStream(model *asr.Model, samples []int16) result {
	if err := model.StreamBegin(context.Background()); err != nil {
		log.Fatalf("stream: %v", err)
	}
	free := time.Duration(0) // when the model is next free, in the recording's time
	var lagSum, lagMax time.Duration
	seen := 0
	for start := 0; start < len(samples); start += step {
		end := min(start+step, len(samples))
		t0 := time.Now()
		if _, err := model.StreamFeed(audio.Floats(samples[start:end])); err != nil {
			log.Fatalf("feed: %v", err)
		}
		free = max(free, at(end)) + time.Since(t0)
		committed := model.Committed()
		for _, t := range committed[seen:] {
			lag := at(end) - t.End
			lagSum += lag
			lagMax = max(lagMax, lag)
		}
		seen = len(committed)
	}
	t0 := time.Now()
	if err := model.StreamFinalize(); err != nil {
		log.Fatalf("finalize: %v", err)
	}
	wait := max(free-at(len(samples)), 0) + time.Since(t0)
	text, err := model.StreamText()
	if err != nil {
		log.Fatalf("stream text: %v", err)
	}
	mean := lagSum / time.Duration(max(seen, 1))
	return result{wait: wait, text: text,
		note: fmt.Sprintf("lag %.1fs, at most %.1fs", mean.Seconds(), lagMax.Seconds())}
}
