package debuglog

import (
	"os"
	"testing"
)

// What the daemon appends is what replay reads back, in order, and the log is
// readable only by its owner.
func TestAppendRead(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if es, err := Read(); err != nil || es != nil {
		t.Fatalf("Read with no log = %v, %v; want nothing", es, err)
	}
	a := Entry{Audio: "a.opus", Text: "one", Phrases: []Stretch{{End: 1, Text: "one"}}, WaitMS: &Wait{Total: 30}}
	b := Entry{Audio: "b.opus"}
	for _, e := range []Entry{a, b} {
		if err := Append(e); err != nil {
			t.Fatal(err)
		}
	}
	es, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 || es[0].Audio != "a.opus" || es[0].WaitMS.Total != 30 || es[1].WaitMS != nil {
		t.Errorf("Read = %+v", es)
	}
	info, err := os.Stat(Dir())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Errorf("Dir mode = %o, want 700", info.Mode().Perm())
	}
}
