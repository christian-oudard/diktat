package ipc

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writePID(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pid")
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadPIDMissingFile(t *testing.T) {
	if got := readPID(filepath.Join(t.TempDir(), "absent")); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestReadPIDGarbage(t *testing.T) {
	if got := readPID(writePID(t, "not-a-number\n")); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestReadPIDLive(t *testing.T) {
	self := os.Getpid()
	if got := readPID(writePID(t, fmt.Sprintf("%d\n", self))); got != self {
		t.Errorf("got %d, want %d", got, self)
	}
}

func TestReadPIDStale(t *testing.T) {
	// Above /proc/sys/kernel/pid_max, so it can never be a live process.
	if got := readPID(writePID(t, "4194305")); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestExePathSelf(t *testing.T) {
	want, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if got := ExePath(os.Getpid()); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
