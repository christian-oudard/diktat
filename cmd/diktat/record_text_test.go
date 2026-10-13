package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	transcribe "github.com/handy-computer/transcribe.cpp/bindings/go"
)

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
