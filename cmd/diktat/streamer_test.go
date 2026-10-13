package main

import (
	"errors"
	"testing"
	"time"
)

// fakeStream commits a word for every whole 100 samples it has been fed, and
// at the end one more for whatever is left over.
func fakeStream() *streamer[[]stamped] {
	fed := 0
	commit := func(n int) []stamped {
		var ws []stamped
		for i := 0; i < n; i++ {
			ws = append(ws, stamped{"w", i * 100, i*100 + 100})
		}
		return ws
	}
	rt := &streamer[[]stamped]{}
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
func pollUntil(t *testing.T, rt *streamer[[]stamped]) ([]stamped, error) {
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

func TestStreamerGivesTheCommittedWords(t *testing.T) {
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

func TestStreamerFinishFeedsTheRestAndEnds(t *testing.T) {
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
func TestStreamerKeepsAudioFromBeforeTheModel(t *testing.T) {
	rt := &streamer[[]stamped]{}
	rt.add(make([]int16, 150))
	if _, changed, _ := rt.poll(); changed {
		t.Fatal("words with no model")
	}
	if ws, _ := rt.finish(); len(ws) != 0 {
		t.Errorf("finish = %+v with no model, want nothing", ws)
	}

	rt = &streamer[[]stamped]{}
	rt.add(make([]int16, 150))
	f := fakeStream()
	rt.ready(f.feed, f.finalize)
	if ws, _ := pollUntil(t, rt); len(ws) != 1 {
		t.Errorf("got %+v, want the word heard before the model loaded", ws)
	}
}

// A failure is said once, and nothing more is fed: a failure that would
// repeat on every feed is not worth repeating.
func TestStreamerStopsAfterAFailure(t *testing.T) {
	rt := &streamer[[]stamped]{}
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

// A feed in flight holds the model, and busy says so until it finishes, so
// a caller that shares the model can wait for it.
func TestStreamerBusyWhileFeeding(t *testing.T) {
	release := make(chan struct{})
	s := &streamer[[]stamped]{}
	s.ready(
		func([]int16) ([]stamped, bool, error) { <-release; return nil, false, nil },
		func() ([]stamped, error) { return nil, nil })
	if s.busy() != nil {
		t.Fatal("busy before anything was fed")
	}
	s.add(make([]int16, 100))
	s.poll()
	busy := s.busy()
	select {
	case <-busy:
		t.Fatal("busy closed while the feed was still running")
	default:
	}
	close(release)
	<-busy
	s.poll()
	if s.busy() != nil {
		t.Error("still busy after the feed finished and nothing more was queued")
	}
}
