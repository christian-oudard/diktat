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
	if err := Select("whisper-base.en"); err != nil {
		t.Fatal(err)
	}
	if got := Selected(); got != "whisper-base.en" {
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

// The state directory is never the config directory: config.toml is the
// user's to write, and nothing here rewrites it. Where each one lands is
// internal/xdg's business; that they differ is this package's.
func TestStateIsNotKeptWithConfig(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	if filepath.Dir(selectedPath()) == filepath.Dir(DefaultPath()) {
		t.Error("state is being kept in the config directory")
	}
}
