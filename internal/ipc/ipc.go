// Package ipc holds the files the diktat commands use to find each other.
package ipc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// These say what diktat is doing, not what was said, so /tmp is the right
// place for them: the status file is read by the bar, which is another
// program under another config, and the rest are a rendezvous rather than
// content. The log records lengths and timings and deliberately never the
// text.
const (
	PIDFile    = "/tmp/diktat-daemon.pid"
	StatusFile = "/tmp/diktat-status"
	LogFile    = "/tmp/diktat-daemon.log"

	// The model directory the daemon currently has loaded. The daemon writes
	// it; diktat-model reads it to report and rewrites it to switch.
	ModelFile = "/tmp/diktat-model"
)

// LastText is the last transcription and LastAudio the capture it came from,
// kept so a bad transcript can be replayed through cmd/transcribe instead of
// being re-recorded.
//
// These two hold what was actually said, so they do not live in /tmp, where
// mode 0644 puts every dictated sentence and a recording of the voice that
// said it within reach of anything else on the machine. XDG_RUNTIME_DIR is
// per-user and mode 0700.
func LastText() (string, error)  { return sessionFile("last") }
func LastAudio() (string, error) { return sessionFile("last.wav") }

// sessionFile names a file in the per-user runtime directory, creating it.
//
// An unset XDG_RUNTIME_DIR is an error rather than a fallback to /tmp: this
// is a Wayland dictation tool, the compositor's own socket lives in that
// directory, and wtype would have nothing to type into without it. Falling
// back would mean quietly writing the private files to the public place in
// the one case the variable is missing.
func sessionFile(name string) (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		return "", errors.New("XDG_RUNTIME_DIR is not set")
	}
	dir := filepath.Join(base, "diktat")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// ReadPID returns the live daemon's PID, or 0 if there is none.
func ReadPID() int {
	return readPID(PIDFile)
}

func readPID(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0
	}
	// Probe liveness with signal 0.
	if syscall.Kill(pid, 0) != nil {
		return 0
	}
	return pid
}

// ExePath returns the binary a process is running. Under nix this is the
// store path, which is what identifies the build.
func ExePath(pid int) string {
	path, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return ""
	}
	return path
}
