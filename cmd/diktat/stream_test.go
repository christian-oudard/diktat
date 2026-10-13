package main

import (
	"strings"
	"testing"

	"github.com/christian-oudard/diktat/internal/asr"
	"github.com/christian-oudard/diktat/internal/audio"
	"github.com/christian-oudard/diktat/internal/models"
	"github.com/christian-oudard/diktat/internal/warmup"
)

// A dictation fed to a streaming model in pieces as it is recorded, through
// the daemon's own tick, comes out as the whole of what was said once the
// stream is finished. Runs only where
// the model is downloaded, since it needs the real thing.
func TestStreamingDictation(t *testing.T) {
	spec, ok := models.Lookup("nemotron-speech-streaming-en-0.6b")
	if !ok || !spec.Downloaded() {
		t.Skip("nemotron-speech-streaming-en-0.6b is not downloaded")
	}
	speech, err := warmup.Speech()
	if err != nil {
		t.Fatal(err)
	}
	m, err := asr.Load(spec.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// install starts a rehearsal, whose result goes back on rehearsed.
	// install starts a rehearsal, whose result goes back on rehearsed.
	fake := &fakeRecorder{}
	d := &daemon{recorder: fake, cfg: emptyConfig(), rehearsed: make(chan bucketResult, 1)}
	d.install(spec.Path(), m)
	d.pauseWarming()
	d.settle()
	if !d.streams {
		t.Fatal("the model does not stream")
	}
	d.startRecording()
	defer d.stopListening()
	samples := audio.Ints(speech)
	step := audio.SampleRate / 4
	for at := 0; at < len(samples); at += step {
		fake.drained = samples[at:min(at+step, len(samples))]
		d.nextPhrase()
		d.settle()
		if d.stream == nil {
			t.Fatal("the stream was given up")
		}
	}
	text, err := d.stream.finish()
	if err != nil {
		t.Fatal(err)
	}
	d.endStream()
	for _, word := range []string{"canoe", "dark blue", "chicken leg", "lemons", "quiet cove"} {
		if !strings.Contains(strings.ToLower(text), word) {
			t.Errorf("transcript %q is missing %q", text, word)
		}
	}
}
