package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The model choice is state, not configuration: diktat writes it, the user
// writes config.toml, and neither touches the other's file.
func TestSelectRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if got := Selected(); got != "" {
		t.Errorf("a fresh state dir remembers %q, want nothing", got)
	}
	if err := Select("parakeet-tdt_ctc-110m"); err != nil {
		t.Fatal(err)
	}
	if got := Selected(); got != "parakeet-tdt_ctc-110m" {
		t.Errorf("Selected() = %q after Select", got)
	}
	// Choosing again replaces rather than appends.
	if err := Select("whisper-tiny.en"); err != nil {
		t.Fatal(err)
	}
	if got := Selected(); got != "whisper-tiny.en" {
		t.Errorf("Selected() = %q after a second Select", got)
	}
}

// Deleting the state has to be safe: it is the way back to the configured
// model, so it must not error or resurrect the old choice.
func TestSelectedAfterDeletion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	if err := Select("canary-1b-flash"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "diktat")); err != nil {
		t.Fatal(err)
	}
	if got := Selected(); got != "" {
		t.Errorf("Selected() = %q after the state was deleted", got)
	}
}

// XDG_STATE_HOME is the tier for "state that persists between restarts", and
// is what the spec says to honour before falling back to ~/.local/state.
func TestStateDirHonoursXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/somewhere/state")
	if got, want := StateDir(), "/somewhere/state/diktat"; got != want {
		t.Errorf("StateDir() = %q, want %q", got, want)
	}
	t.Setenv("XDG_STATE_HOME", "")
	if got := StateDir(); filepath.Base(got) != "diktat" ||
		filepath.Base(filepath.Dir(got)) != "state" {
		t.Errorf("StateDir() = %q, want a .local/state/diktat path", got)
	}
	// Never the config directory: that file is the user's to write.
	if StateDir() == filepath.Dir(DefaultPath()) {
		t.Error("state is being kept in the config directory")
	}
}
