package main

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/christian-oudard/diktat/internal/transcript"
)

func someJob() job {
	return job{Audio: "/tmp/meeting.opus", Size: 1234, Modified: 5678,
		Pipeline: "parakeet-tdt-0.6b-v2 + sortformer"}
}

func TestResumeReturnsSavedWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.partial.json")
	c := resume(path, someJob())
	c.Rows = []transcript.Row{{Speaker: 1, Start: 0, End: 2 * time.Second}}
	c.Pieces = append(c.Pieces, []transcript.Span{{Start: time.Second, End: 2 * time.Second, Text: "hello"}})
	c.save()

	got := resume(path, someJob())
	if len(got.Rows) != 1 || got.Rows[0] != c.Rows[0] {
		t.Errorf("rows %+v, want %+v", got.Rows, c.Rows)
	}
	if len(got.Pieces) != 1 || len(got.Pieces[0]) != 1 || got.Pieces[0][0] != c.Pieces[0][0] {
		t.Errorf("pieces %+v, want %+v", got.Pieces, c.Pieces)
	}
}

// Work saved for one run is wrong for any other: a different file, a file
// changed since, or a different pipeline or option all change the text.
func TestResumeStartsOverForAnotherJob(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.partial.json")
	c := resume(path, someJob())
	c.Pieces = append(c.Pieces, []transcript.Span{{Text: "hello"}})
	c.save()

	other := someJob()
	other.Modified++
	got := resume(path, other)
	if len(got.Pieces) != 0 || got.Rows != nil {
		t.Errorf("resumed %+v for a changed file", got)
	}
}

func TestResumeWithNothingSaved(t *testing.T) {
	got := resume(filepath.Join(t.TempDir(), "x.partial.json"), someJob())
	if len(got.Pieces) != 0 || got.Rows != nil {
		t.Errorf("resumed %+v from no file", got)
	}
}

func TestSay(t *testing.T) {
	var b bytes.Buffer
	say(&b, []transcript.Turn{
		{Speaker: 1, Start: 3 * time.Second, Text: "Good morning, everyone."},
		{Speaker: 2, Start: 67 * time.Minute, Text: "Morning."},
	})
	want := "S01  0:00:03  Good morning, everyone.\nS02  1:07:00  Morning.\n"
	if b.String() != want {
		t.Errorf("got\n%s\nwant\n%s", b.String(), want)
	}
}

// A piece's text is the audio between two cuts, so text saved by a run that
// cut elsewhere is not the text of this run's pieces.
func TestCutAtKeepsPiecesOnlyForTheSameCuts(t *testing.T) {
	c := resume(filepath.Join(t.TempDir(), "x.partial.json"), someJob())
	c.cutAt([]int{0, 100, 200})
	c.Pieces = [][]transcript.Span{{{Text: "a"}}, {{Text: "b"}}}

	c.cutAt([]int{0, 100, 200})
	if len(c.Pieces) != 2 {
		t.Errorf("dropped %d pieces cut the same way", 2-len(c.Pieces))
	}
	c.cutAt([]int{0, 50, 100, 150, 200})
	if len(c.Pieces) != 0 {
		t.Errorf("kept %d pieces cut another way", len(c.Pieces))
	}
}
