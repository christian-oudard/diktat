package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"

	"github.com/christian-oudard/diktat/internal/transcript"
)

// job is everything a run's text depends on, which is what decides whether
// saved work belongs to it. The file is named by its path and identified by
// size and modification time, since hashing an hour of audio to compare it
// costs more than the check is worth.
type job struct {
	Audio    string
	Size     int64
	Modified int64
	Pipeline string
	Language string
	Speakers int
}

// jobFor is the job a run over this recording with these options would be.
func jobFor(audio, pipeline, language string, speakers int) job {
	abs, err := filepath.Abs(audio)
	if err != nil {
		log.Fatal(err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		log.Fatal(err)
	}
	return job{Audio: abs, Size: info.Size(), Modified: info.ModTime().UnixNano(),
		Pipeline: pipeline, Language: language, Speakers: speakers}
}

// checkpoint is a run's finished work, saved as it is done, so a run that is
// stopped picks up where it left off.
type checkpoint struct {
	path string
	Job  job
	// Rows is who spoke when, from the diarizer's one pass over the whole
	// recording, or nil before that pass has run.
	Rows []transcript.Row
	// Cuts is where each piece starts, in samples, and Pieces the text of
	// each piece transcribed so far, in order.
	Cuts   []int
	Pieces [][]transcript.Span
}

// resume is the work saved at path for this job, or none if what is there was
// saved for another.
func resume(path string, j job) *checkpoint {
	c := &checkpoint{path: path, Job: j}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c
	}
	if err != nil {
		log.Fatal(err)
	}
	var saved checkpoint
	if err := json.Unmarshal(data, &saved); err != nil {
		log.Fatalf("%s: %v", path, err)
	}
	if saved.Job != j {
		log.Printf("%s was saved by a different run, starting over", path)
		return c
	}
	saved.path = path
	return &saved
}

// cutAt keeps the saved text only if this run cuts the recording where the
// saving run did, since a piece's text is the audio between two cuts.
func (c *checkpoint) cutAt(cuts []int) {
	if slices.Equal(c.Cuts, cuts) {
		return
	}
	if len(c.Pieces) > 0 {
		log.Printf("%s was cut into other pieces, transcribing them again", c.path)
	}
	c.Cuts, c.Pieces = cuts, nil
}

// save writes the work so far. Through a rename, so a run stopped mid-write
// leaves the previous save rather than half of this one.
func (c *checkpoint) save() {
	data, err := json.Marshal(c)
	if err != nil {
		log.Fatal(err)
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		log.Fatalf("save progress: %v", err)
	}
	if err := os.Rename(tmp, c.path); err != nil {
		log.Fatalf("save progress: %v", err)
	}
}

// say prints turns as they are transcribed, one line each.
func say(w io.Writer, turns []transcript.Turn) {
	for _, t := range turns {
		fmt.Fprintf(w, "S%02d  %s  %s\n", t.Speaker, transcript.Stamp(t.Start), t.Text)
	}
}
