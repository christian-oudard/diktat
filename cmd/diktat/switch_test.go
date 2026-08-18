package main

import (
	"errors"
	"testing"
	"time"

	"github.com/christian-oudard/diktat/internal/asr"
)

// A switch answers the most recent request. These cover which of them needs a
// load, which needs none, and which has to stop the load already running,
// because the answer used to be to queue behind that load: asking for a third
// model while a 2 GB second one loaded waited out the whole of a load nobody
// wanted, warmup included.

func TestPlan(t *testing.T) {
	const (
		inUse   = "/models/in-use"
		other   = "/models/other"
		loading = "/models/loading"
	)
	for _, c := range []struct {
		name      string
		req       string
		loading   string
		cancelled bool
		want      step
	}{
		{name: "the model in use", req: inUse, want: stepNothing},
		{name: "another model", req: other, want: stepLoad},
		// The model file names the model still in use while a load is in
		// flight, so a second ask for the model being loaded looks new.
		{name: "asked for twice", req: loading, loading: loading, want: stepWait},
		{name: "a third model", req: other, loading: loading, want: stepCancel},
		// Ordering: this one has nothing against the load in flight, so it
		// may not be answered by cancelling and reloading.
		{name: "back to the model in use", req: inUse, loading: loading, want: stepNothing},
		// Once cancelled, that load will produce nothing, so asking for it
		// again has to start it over rather than wait for it.
		{name: "asked for again after cancelling",
			req: loading, loading: loading, cancelled: true, want: stepCancel},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := plan(c.req, inUse, c.loading, c.cancelled)
			if got != c.want {
				t.Errorf("plan(%s) = %v, want %v", c.req, got, c.want)
			}
		})
	}
}

// A model is usable before it is warm, so the rehearsal runs in the gaps
// between dictations and gets interrupted by them. These cover what happens to
// a length that was interrupted, which has to be run again: giving it up would
// leave a hole in the coverage, and counting it would be a lie about a run
// that may have stopped before it compiled anything.
func TestRehearsalRecordsProgress(t *testing.T) {
	w := &rehearsal{buckets: []int{1, 2, 3}}

	if !w.record(bucketResult{secs: 1, took: time.Second}) {
		t.Fatal("a finished length ended the rehearsal")
	}
	if w.next != 1 || w.spent != time.Second || len(w.work) != 1 {
		t.Errorf("after one length: next %d, spent %s, work %v", w.next, w.spent, w.work)
	}

	// Interrupted by a dictation. The length comes round again, and nothing
	// about it is counted.
	if !w.record(bucketResult{secs: 2, took: 50 * time.Millisecond, err: asr.ErrAborted}) {
		t.Error("a cancelled length ended the rehearsal")
	}
	if w.next != 1 || w.spent != time.Second || len(w.work) != 1 {
		t.Errorf("a cancelled length was counted: next %d, spent %s, work %v", w.next, w.spent, w.work)
	}

	// A real failure is not worth retrying: it would fail the same way every
	// time, which on a card is a busy loop rather than a warmup.
	if w.record(bucketResult{secs: 2, err: errors.New("no")}) {
		t.Error("a failing length was left on the list")
	}
}

// The daemon's own bookkeeping around that: what dropLoad leaves behind is
// what tells a later request that the load in flight is already dying.
func TestDropLoad(t *testing.T) {
	stopped := 0
	d := &daemon{loading: "/models/loading", cancel: func() { stopped++ }}

	d.dropLoad("/models/wanted")
	if stopped != 1 {
		t.Errorf("dropLoad did not stop the load (%d calls)", stopped)
	}
	if d.wanted != "/models/wanted" {
		t.Errorf("wanted = %q, want the model asked for", d.wanted)
	}
	if d.cancel != nil {
		t.Error("the spent cancel was kept, so a later ask cannot tell the load is dying")
	}

	// A second request replaces what the cancelled load will be followed by.
	// The load is already stopping, so there is nothing left to cancel, but
	// the newer request still has to be the one honoured.
	d.dropLoad("")
	if stopped != 1 {
		t.Errorf("cancel called again (%d calls)", stopped)
	}
	if d.wanted != "" {
		t.Errorf("wanted = %q, want the newer request to win", d.wanted)
	}

	// Nothing loading, nothing to drop.
	idle := &daemon{wanted: "untouched"}
	idle.dropLoad("")
	if idle.wanted != "untouched" {
		t.Errorf("dropLoad touched an idle daemon: wanted = %q", idle.wanted)
	}
}
