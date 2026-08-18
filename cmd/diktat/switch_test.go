package main

import "testing"

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
		resident  bool
		cancelled bool
		want      step
	}{
		{name: "the model in use", req: inUse, want: stepNothing},
		{name: "resident already", req: other, resident: true, want: stepInstall},
		{name: "not resident", req: other, want: stepLoad},
		// The model file names the model still in use while a load is in
		// flight, so a second ask for the model being loaded looks new.
		{name: "asked for twice", req: loading, loading: loading, want: stepWait},
		{name: "a third model", req: other, loading: loading, want: stepCancel},
		// Ordering: neither of these has anything against the load in flight,
		// so neither may be answered by cancelling and reloading.
		{name: "back to the model in use", req: inUse, loading: loading, want: stepNothing},
		{name: "back to a resident one", req: other, loading: loading, resident: true, want: stepInstall},
		// Once cancelled, that load will produce nothing, so asking for it
		// again has to start it over rather than wait for it.
		{name: "asked for again after cancelling",
			req: loading, loading: loading, cancelled: true, want: stepCancel},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := plan(c.req, inUse, c.loading, c.resident, c.cancelled)
			if got != c.want {
				t.Errorf("plan(%s) = %v, want %v", c.req, got, c.want)
			}
		})
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
