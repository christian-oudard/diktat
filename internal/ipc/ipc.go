// Package ipc holds the files the diktat commands use to find each other.
package ipc

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

const (
	PIDFile      = "/tmp/diktat-daemon.pid"
	StatusFile   = "/tmp/diktat-status"
	LastTextFile = "/tmp/diktat-last"
	LogFile      = "/tmp/diktat-daemon.log"

	// The audio of the last capture, exactly as handed to the model, so an
	// utterance that transcribed badly can be replayed through
	// diktat-transcribe instead of being re-recorded.
	LastAudioFile = "/tmp/diktat-last.wav"

	// The model directory the daemon currently has loaded. The daemon writes
	// it; diktat-model reads it to report and rewrites it to switch.
	ModelFile = "/tmp/diktat-model"
)

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
