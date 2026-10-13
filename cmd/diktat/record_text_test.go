package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	transcribe "github.com/handy-computer/transcribe.cpp/bindings/go"
)

// fakeStream commits a word for every whole 100 samples it has been fed, and
// at the end one more for whatever is left over.
func fakeStream() *recordText {
	fed := 0
	commit := func(n int) []stamped {
		var ws []stamped
		for i := 0; i < n; i++ {
			ws = append(ws, stamped{"w", i * 100, i*100 + 100})
		}
		return ws
	}
	rt := &recordText{}
	rt.ready(
		func(s []int16) ([]stamped, bool, error) {
			before := fed / 100
			fed += len(s)
			return commit(fed / 100), fed/100 > before, nil
		},
		func() ([]stamped, error) { return commit((fed + 99) / 100), nil })
	return rt
}

// pollUntil polls until the words change or a second passes.
func pollUntil(t *testing.T, rt *recordText) ([]stamped, error) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ws, changed, err := rt.poll(); changed || err != nil {
			return ws, err
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no words came")
	return nil, nil
}

func TestRecordTextGivesTheCommittedWords(t *testing.T) {
	rt := fakeStream()
	rt.add(make([]int16, 250))
	if ws, err := pollUntil(t, rt); err != nil || len(ws) != 2 {
		t.Fatalf("got %+v, %v; want the two words in 250 samples", ws, err)
	}
	rt.add(make([]int16, 100))
	if ws, _ := pollUntil(t, rt); len(ws) != 3 || ws[2].start != 200 {
		t.Errorf("got %+v, want a third word from 200", ws)
	}
}

func TestRecordTextFinishFeedsTheRestAndEnds(t *testing.T) {
	rt := fakeStream()
	rt.add(make([]int16, 150))
	rt.poll() // starts a feed of the 150, which finish must wait for
	rt.add(make([]int16, 100))
	ws, err := rt.finish()
	if err != nil || len(ws) != 3 {
		t.Errorf("finish = %+v, %v; want three words for 250 samples", ws, err)
	}
}

// Audio heard before the model loads is not lost: it is fed first.
func TestRecordTextKeepsAudioFromBeforeTheModel(t *testing.T) {
	rt := &recordText{}
	rt.add(make([]int16, 150))
	if _, changed, _ := rt.poll(); changed {
		t.Fatal("words with no model")
	}
	if ws, _ := rt.finish(); len(ws) != 0 {
		t.Errorf("finish = %+v with no model, want nothing", ws)
	}

	rt = &recordText{}
	rt.add(make([]int16, 150))
	f := fakeStream()
	rt.ready(f.feed, f.finalize)
	if ws, _ := pollUntil(t, rt); len(ws) != 1 {
		t.Errorf("got %+v, want the word heard before the model loaded", ws)
	}
}

// A failure is said once, and nothing more is fed: a failure that would
// repeat on every feed is not worth repeating.
func TestRecordTextStopsAfterAFailure(t *testing.T) {
	rt := &recordText{}
	rt.ready(
		func(s []int16) ([]stamped, bool, error) { return nil, false, errors.New("broken") },
		func() ([]stamped, error) { t.Error("finalized after a failure"); return nil, nil })
	rt.add(make([]int16, 150))
	if _, err := pollUntil(t, rt); err == nil {
		t.Fatal("no error from a failing model")
	}
	if ws, err := rt.finish(); len(ws) != 0 || err != nil {
		t.Errorf("finish = %+v, %v after a failure, want nothing", ws, err)
	}
}

// A word is every token from one that starts with a space to the next, timed
// from the first token's start to the last one's end, in samples.
func TestWordsOfTokens(t *testing.T) {
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	ws := wordsOf([]transcribe.Token{
		{Text: " Ch", Start: ms(1280), End: ms(1360)},
		{Text: "e", Start: ms(1280), End: ms(1360)},
		{Text: "ck", Start: ms(1360), End: ms(1440)},
		{Text: ",", Start: ms(1440), End: ms(1520)},
		{Text: " one", Start: ms(1600), End: ms(1680)},
	})
	want := []stamped{{"Check,", 20480, 24320}, {"one", 25600, 26880}}
	if len(ws) != 2 || ws[0] != want[0] || ws[1] != want[1] {
		t.Errorf("got %+v, want %+v", ws, want)
	}
}

// Off a terminal, a word is printed once the next one shows it is finished,
// and each sentence gets its own line.
func TestPrinterOneSentencePerLine(t *testing.T) {
	var out bytes.Buffer
	p := &printer{w: &out}
	words := func(texts ...string) []stamped {
		var ws []stamped
		for _, s := range texts {
			ws = append(ws, stamped{text: s})
		}
		return ws
	}
	p.show(words("Hello", "there."))
	if out.String() != "Hello" {
		t.Errorf("got %q, want only the finished word", out.String())
	}
	p.show(words("Hello", "there.", "How", "are"))
	p.close(words("Hello", "there.", "How", "are", "you?"))
	if want := "Hello there.\nHow are you?\n"; out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
	if strings.Count(out.String(), "Hello") != 1 {
		t.Error("a word printed twice")
	}
}
