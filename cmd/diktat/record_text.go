package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	transcribe "github.com/handy-computer/transcribe.cpp/bindings/go"

	"github.com/christian-oudard/diktat/internal/asr"
	"github.com/christian-oudard/diktat/internal/audio"
	"github.com/christian-oudard/diktat/internal/config"
	"github.com/christian-oudard/diktat/internal/models"
)

// wordsOf joins tokens into words: a token that starts with a space starts a
// word. A word runs from its first token's start to its last one's end.
func wordsOf(tokens []transcribe.Token) []stamped {
	var words []stamped
	for _, t := range tokens {
		start, end := samplesIn(t.Start), samplesIn(t.End)
		if len(words) == 0 || strings.HasPrefix(t.Text, " ") {
			words = append(words, stamped{strings.TrimSpace(t.Text), start, end})
			continue
		}
		w := &words[len(words)-1]
		w.text += t.Text
		w.end = max(w.end, end)
	}
	return words
}

func samplesIn(d time.Duration) int { return int(d * audio.SampleRate / time.Second) }

// printer writes the text where there is no board to draw it on: each word
// once the next one shows it is finished, a sentence to a line.
type printer struct {
	w    io.Writer
	done int
	// open is whether the line written last is still to be ended.
	open bool
}

func (p *printer) show(words []stamped) {
	for ; p.done < len(words)-1; p.done++ {
		p.print(words[p.done].text)
	}
}

// close prints the last word, which nothing after it will finish.
func (p *printer) close(words []stamped) {
	for ; p.done < len(words); p.done++ {
		p.print(words[p.done].text)
	}
	if p.open {
		fmt.Fprintln(p.w)
	}
}

func (p *printer) print(word string) {
	if p.open {
		fmt.Fprint(p.w, " ")
	}
	fmt.Fprint(p.w, word)
	p.open = !strings.ContainsAny(word[len(word)-1:], ".?!")
	if !p.open {
		fmt.Fprintln(p.w)
	}
}

// checkTextModel says whether --text can run, without loading anything, and
// if not, what to do about it.
func checkTextModel() error {
	name := config.StartModel()
	if s, ok := models.Lookup(name); ok && !s.Streams() {
		return fmt.Errorf("--text transcribes as you speak, which %s cannot.\n"+
			"Choose one that can with: diktat model %s", s.Name, streamingModel())
	}
	if err := models.Check(models.Resolve(name)); err != nil {
		return fmt.Errorf("--text needs %v\nFetch it with: diktat model %s", err, name)
	}
	return nil
}

// streamingModel is the menu's entry that streams.
func streamingModel() string {
	for _, s := range models.Catalog {
		if s.Streams() {
			return s.Name
		}
	}
	panic("no model on the menu streams")
}

// loadTextModel loads the dictation model and starts its stream.
func loadTextModel() (*asr.Model, error) {
	m, err := asr.Load(models.Resolve(config.StartModel()))
	if err != nil {
		return nil, err
	}
	// A model given by path is not on the menu, so this is the first that
	// is known of it.
	streams, err := m.Streams()
	if err == nil && !streams {
		err = fmt.Errorf("%s does not stream, which --text needs", m.Name())
	}
	if err == nil {
		err = m.StreamBegin(context.Background())
	}
	if err != nil {
		m.Close()
		return nil, err
	}
	return m, nil
}

// feedText and finalizeText are the two calls a streamer makes on a streaming
// model for record --text.
func feedText(m *asr.Model) func([]int16) ([]stamped, bool, error) {
	return func(samples []int16) ([]stamped, bool, error) {
		changed, err := m.StreamFeed(audio.Floats(samples))
		if err != nil || !changed {
			return nil, false, err
		}
		return wordsOf(m.Committed()), true, nil
	}
}

func finalizeText(m *asr.Model) func() ([]stamped, error) {
	return func() ([]stamped, error) {
		if err := m.StreamFinalize(); err != nil {
			return nil, err
		}
		return wordsOf(m.Committed()), nil
	}
}
