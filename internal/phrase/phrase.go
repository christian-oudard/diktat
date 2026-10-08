// Package phrase cuts a dictation still being recorded at the pauses in it,
// so what was said before a pause can be transcribed while the speaker
// carries on. See docs/phrases.md.
package phrase

import (
	"context"
	"log"
	"strings"
	"time"

	transcribe "github.com/handy-computer/transcribe.cpp/bindings/go"

	"github.com/christian-oudard/diktat/internal/asr"
	"github.com/christian-oudard/diktat/internal/audio"
	"github.com/christian-oudard/diktat/internal/models"
	"github.com/christian-oudard/diktat/internal/silence"
)

const (
	// pause is the silence that ends a phrase. Shorter than the pause between
	// sentences, which is what makes the cut land between them rather than
	// inside one.
	pause = 800 * time.Millisecond
	// shortest is the least audio worth transcribing as a phrase. A model
	// reads a word by the words around it, so a phrase much shorter than a
	// sentence costs more in the words either side of the cut than it saves.
	shortest = 4 * time.Second
)

// Detector finds where the finished phrases in a recording end.
type Detector struct {
	model *asr.Model
}

// LoadDetector opens the voice activity detector, or returns the error saying
// it is not downloaded.
func LoadDetector() (*Detector, error) {
	path := models.Detector.Path()
	if err := models.Check(path); err != nil {
		return nil, err
	}
	m, err := asr.Load(path)
	if err != nil {
		return nil, err
	}
	return &Detector{m}, nil
}

func (d *Detector) Close() { d.model.Close() }

// End is the number of samples at the start of a recording still in progress
// that hold finished phrases, or 0 if there are none yet.
func (d *Detector) End(ctx context.Context, samples []int16) (int, error) {
	res, err := d.model.Run(ctx, audio.Floats(samples), &transcribe.RunOptions{Diarize: transcribe.ModeOn})
	if err != nil {
		return 0, err
	}
	speech := make([]silence.Span, len(res.SpeakerSegments))
	for i, r := range res.SpeakerSegments {
		speech[i] = silence.Span{Start: r.Start, End: r.End}
	}
	length := time.Duration(len(samples)) * time.Second / audio.SampleRate
	return int(end(speech, length) * audio.SampleRate / time.Second), nil
}

// end is where the last pause of at least pause, after at least shortest of
// audio, is half over, or 0 if there is no such pause. The silence after the
// last region counts as a pause, since it is one that has not ended yet.
func end(speech []silence.Span, length time.Duration) time.Duration {
	for i := len(speech) - 1; i >= 0; i-- {
		next := length
		if i+1 < len(speech) {
			next = speech[i+1].Start
		}
		cut := (speech[i].End + next) / 2
		if next-speech[i].End >= pause && cut >= shortest {
			return cut
		}
	}
	return 0
}

// Transcribe transcribes one stretch of a dictation. Only what the model
// would refuse, or what the card cannot hold, is cut, at the quietest moment
// near the limit: most families take the whole of it and window it
// themselves, which they do better than a cut here can.
func Transcribe(ctx context.Context, m *asr.Model, samples []int16) (string, error) {
	limit := m.AudioLimit()
	chunks := audio.Chunk(samples, int(limit.Seconds())*audio.SampleRate)
	if len(chunks) > 1 {
		log.Printf("Over the %s this model can take now, transcribing in %d pieces",
			limit.Round(time.Second), len(chunks))
	}
	var parts []string
	for _, chunk := range chunks {
		part, err := m.Transcribe(ctx, audio.Pad(audio.Floats(chunk)))
		if err != nil {
			return "", err
		}
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " "), nil
}
