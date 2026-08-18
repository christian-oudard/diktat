package main

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/christian-oudard/diktat/internal/asr"
	"github.com/christian-oudard/diktat/internal/suspend"
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

// A model whose weights are gone from the card does not fail, it goes quiet:
// the graphs still run at their usual speed and every dictation comes back
// empty. A suspend does that, and the daemon holds one model for the whole
// session, so before this it stayed quiet until someone noticed and restarted
// it. The wake run is the one run all session whose words are known ahead of
// time, so its answer is what says which of the two silences this is.
func TestMute(t *testing.T) {
	for _, c := range []struct {
		name     string
		probe    string
		answered bool
		want     bool
	}{
		{name: "words, as ever", probe: "the birch canoe", answered: true},
		{name: "silent where it used to speak", answered: true, want: true},
		// Never having had words for the clip is not a change, and reloading on
		// it would reload before every dictation for as long as that model was
		// in use.
		{name: "silent and always was"},
		{name: "words for the first time", probe: "the birch canoe"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := mute(c.probe, c.answered); got != c.want {
				t.Errorf("mute(%q, %v) = %v, want %v", c.probe, c.answered, got, c.want)
			}
		})
	}
}

// Where that baseline comes from. Taking it from the first dictation is not
// enough: a machine suspended before anyone dictated leaves a model that has
// never answered, so the silence afterwards is not a change from anything and
// the daemon would stay mute for the whole session. The rehearsal runs the
// same known speech within a second or two of every load, so it answers first.
func TestFinishBucketAnswers(t *testing.T) {
	statusPath = filepath.Join(t.TempDir(), "status")
	activityPath = filepath.Join(t.TempDir(), "activity")

	// A load in flight is what keeps finishBucket from starting the next
	// length on a model these tests do not have.
	inUse, other := new(asr.Model), new(asr.Model)
	d := &daemon{model: inUse, loading: "/models/loading",
		warming: &rehearsal{model: inUse, buckets: []int{1, 2}}}

	d.finishBucket(bucketResult{model: inUse, secs: 1, text: "the birch canoe"})
	if !d.answered {
		t.Error("a rehearsal with words for the clip did not set the baseline")
	}

	// A length that lands after a switch describes the model it ran on, which
	// is no longer the one a later silence would be judged against.
	d = &daemon{model: inUse, loading: "/models/loading",
		warming: &rehearsal{model: other, buckets: []int{1, 2}}}
	d.finishBucket(bucketResult{model: other, secs: 1, text: "the birch canoe"})
	if d.answered {
		t.Error("a rehearsal of the model just replaced set the baseline for its replacement")
	}
}

// A suspend discards the card's memory unless the driver was told to save it,
// so the daemon watches the kernel's ledger of sleep and reloads when it
// moves. These cover the bookkeeping; the reload itself needs a card.
func TestCheckSuspend(t *testing.T) {
	statusPath = filepath.Join(t.TempDir(), "status")
	activityPath = filepath.Join(t.TempDir(), "activity")

	// Nothing slept: nothing to notice.
	d := &daemon{asleep: suspend.Total()}
	d.checkSuspend()
	if d.suspends != 0 || d.loading != "" {
		t.Errorf("a machine that never slept was treated as resumed: suspends %d, loading %q",
			d.suspends, d.loading)
	}

	// The machine slept, but the model lives in RAM, which a suspend preserves
	// by definition. The sleep is still counted, since a load in flight is
	// judged against the count.
	d = &daemon{model: new(asr.Model), asleep: suspend.Total() - time.Hour}
	d.checkSuspend()
	if d.suspends != 1 {
		t.Errorf("suspends = %d, want the sleep counted", d.suspends)
	}
	if d.loading != "" {
		t.Errorf("a CPU model was reloaded after a resume: loading %q", d.loading)
	}
	// One sleep is one sleep, not one per look after it.
	d.checkSuspend()
	if d.suspends != 1 {
		t.Errorf("one sleep counted twice: suspends = %d", d.suspends)
	}
}

// A load that was reading the card when the machine slept may describe memory
// that is already gone, so what it produced is closed unopened and the load
// runs again, unless something newer was asked for meanwhile, whose model
// replaces this one anyway.
func TestFinishLoadStaleGeneration(t *testing.T) {
	statusPath = filepath.Join(t.TempDir(), "status")
	activityPath = filepath.Join(t.TempDir(), "activity")

	d := &daemon{suspends: 1, loaded: make(chan loadResult, 1)}
	d.finishLoad(loadResult{dir: "/models/stale", model: new(asr.Model), gen: 0})
	if d.loading != "/models/stale" {
		t.Errorf("loading = %q, want the stale load started over", d.loading)
	}

	d = &daemon{suspends: 1, loaded: make(chan loadResult, 1), wanted: "/models/newer"}
	d.finishLoad(loadResult{dir: "/models/stale", model: new(asr.Model), gen: 0})
	if d.loading != "/models/newer" {
		t.Errorf("loading = %q, want the newer request honoured over the rerun", d.loading)
	}
}

// The daemon's bookkeeping around that: a probe is judged once, and a model
// that has spoken is remembered as having spoken.
func TestCheckProbeRemembersAnswers(t *testing.T) {
	d := &daemon{probing: true, probe: "the birch canoe"}
	d.checkProbe()
	if !d.answered {
		t.Error("an answered probe was not remembered")
	}
	if d.probing {
		t.Error("the probe was left to be judged a second time")
	}

	// A run that was not the wake run leaves no probe to judge, and the stale
	// answer from the last one must not be read as this one's.
	d = &daemon{answered: true}
	d.checkProbe()
	if !d.answered {
		t.Error("a daemon with no probe in flight was judged anyway")
	}
}

// The daemon's log goes to stderr and nothing else, which under systemd is the
// journal. The journal stamps what it captures, so stamping it here as well
// would put two times on every line; run from a terminal nobody stamps it at
// all, which is poor for a log whose subject is how long things took.
func TestLogFlags(t *testing.T) {
	t.Setenv("JOURNAL_STREAM", "8:1234")
	if got := logFlags(); got != 0 {
		t.Errorf("logFlags() = %d under systemd, want 0: the journal stamps its own", got)
	}
	t.Setenv("JOURNAL_STREAM", "")
	if got := logFlags(); got != log.LstdFlags {
		t.Errorf("logFlags() = %d in a terminal, want %d", got, log.LstdFlags)
	}
}

// Dictating is the common case and it happened several times a minute, so the
// lines describing how a transcription went buried the ones saying a switch or
// a suspend had happened. Those are debug now, and the gate is read once.
func TestDebugf(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer log.SetOutput(os.Stderr)

	debugEnabled = false
	debugf("mel 8ms encode 241ms")
	if buf.Len() != 0 {
		t.Errorf("debug line logged by default: %q", buf.String())
	}

	debugEnabled = true
	defer func() { debugEnabled = false }()
	debugf("mel 8ms encode 241ms")
	if got := strings.TrimSpace(buf.String()); got != "mel 8ms encode 241ms" {
		t.Errorf("debugf with DIKTAT_DEBUG set = %q, want the line", got)
	}
}
