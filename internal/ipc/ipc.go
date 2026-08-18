// Package ipc holds the files the diktat commands use to find each other.
package ipc

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/christian-oudard/diktat/internal/xdg"
)

// None of these are in /tmp any more. Fixed names in a shared directory meant
// a second user on the machine had nothing to write to, since the name was
// taken by a file they did not own, and mode 0644 published what diktat was
// doing to everything else running.
//
// The log is not among them either. It goes to stderr, which is the journal
// when systemd starts the daemon, and the terminal when a person does.

// PIDPath holds the daemon's PID, written at startup and removed at exit.
// `diktat toggle` reads it to know where to send its signal.
func PIDPath() (string, error) { return sessionFile("daemon.pid") }

// ModelPath holds the model directory the daemon currently has loaded. The
// daemon writes it; `diktat model` reads it to report and rewrites it to ask
// for a switch.
func ModelPath() (string, error) { return sessionFile("model") }

// ActivityPath holds what the daemon is busy with, as a word and the model
// directory it applies to -- "loading <dir>" or "warming <dir>" -- and is
// absent when it is busy with neither. The daemon writes it; `diktat model`
// reads it.
//
// Separate from ModelPath because the two answer different questions and both
// are asked at once: that one says which model a dictation right now would
// use, this one says what is happening to make that better. Without it the
// menu could not tell "not switched yet" from "not switching", and a model
// that works but is still rehearsing looked identical to one that was done.
func ActivityPath() (string, error) { return sessionFile("activity") }

// LastText is the last transcription, kept so `diktat repeat` can type it
// again.
//
// It holds what was actually said, which is the reason this directory is mode
// 0700 rather than somewhere every process on the machine can read.
//
// The capture itself is not kept. Writing a recording of someone's voice on
// every dictation earns its keep only if the recording gets replayed, and
// nothing here replays it: cmd/transcribe takes files someone chose to make.
func LastText() (string, error) { return sessionFile("last") }

// StatusPath holds a Pango markup string saying what the daemon is doing.
//
// It is the one file here another program reads, so it is the one with a path
// a person has to be able to type into a bar's config. That rules out the
// runtime directory, which is /run/user/<uid> and cannot be written as ~, and
// it is why this sits with the state a session outlives rather than with the
// rendezvous files below. The cost is that a daemon killed outright leaves the
// last thing it was doing on screen, where a runtime file would have gone at
// logout; the next start overwrites it.
func StatusPath() (string, error) {
	dir := xdg.StateDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "status"), nil
}

// sessionFile names a file in the per-user runtime directory, creating it.
// What lives there is exactly as long-lived as the session: logind empties it
// when the user's last one ends, so a daemon that died without cleaning up
// leaves nothing for the next session to read as live.
func sessionFile(name string) (string, error) {
	dir, err := xdg.RuntimeDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// ReadPID returns the live daemon's PID, or 0 if there is none.
func ReadPID() int {
	path, err := PIDPath()
	if err != nil {
		return 0
	}
	return readPID(path)
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
