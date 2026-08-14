package ipc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The files holding what was said must land somewhere only this user can
// read. Getting that wrong is silent: the transcript still works, it is just
// also readable by everything else on the machine.
func TestSessionFileIsPrivate(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	path, err := LastText()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0700 {
		t.Errorf("session directory is %04o, want 0700", perm)
	}
}

// An unset XDG_RUNTIME_DIR must fail rather than fall back, since the only
// fallback is the public directory these files exist to stay out of.
func TestSessionFileNeedsRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")

	for _, f := range []struct {
		name string
		fn   func() (string, error)
	}{{"LastText", LastText}} {
		path, err := f.fn()
		if err == nil {
			t.Errorf("%s() = %q with no XDG_RUNTIME_DIR, want an error", f.name, path)
		}
		if strings.HasPrefix(path, "/tmp") {
			t.Errorf("%s() fell back to %q", f.name, path)
		}
	}
}
