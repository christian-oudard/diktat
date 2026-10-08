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

// A live process that is not diktat is not the daemon. PIDs are recycled, and
// the signal toggle sends terminates a process that is not expecting it.
func TestReadPIDLiveButNotDiktat(t *testing.T) {
	// PID 1 is always alive and is never diktat. Reading another user's
	// /proc/<pid>/exe fails, which is itself the answer: a PID this cannot
	// identify is not one to signal.
	if got := readPID(writePID(t, "1\n")); got != 0 {
		t.Errorf("got %d, want 0: PID 1 is not the diktat daemon", got)
	}
}

// Another build of diktat is the daemon too, which is what an upgrade leaves
// running. Under nix the running binary is the wrapper's target, not the
// name on PATH.
func TestIsDiktat(t *testing.T) {
	for exe, want := range map[string]bool{
		"/nix/store/abc-diktat-1.0.0/bin/diktat":          true,
		"/nix/store/abc-diktat-1.0.0/bin/.diktat-wrapped": true,
		"/home/someone/go/bin/diktat":                     true,
		"/usr/bin/python3":                                false,
		"/usr/bin/.python3-wrapped":                       false,
	} {
		if got := isDiktat(exe); got != want {
			t.Errorf("isDiktat(%q) = %v, want %v", exe, got, want)
		}
	}
}

// A reader sees the old contents or the new ones, never a truncated file: the
// daemon rewrites these while other processes are reading them, and half a
// sentence typed by `diktat repeat` is what the old truncate-then-write cost.
func TestWriteIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last")
	first := []byte("the first sentence, which is long enough to notice")
	if err := Write(path, first, 0600); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			_ = Write(path, []byte("the second sentence, also long"), 0600)
			_ = Write(path, first, 0600)
		}
	}()
	for i := 0; i < 2000; i++ {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read while writing: %v", err)
		}
		if s := string(raw); s != string(first) && s != "the second sentence, also long" {
			t.Fatalf("read a partial file: %q", s)
		}
	}
	<-done

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("mode is %o after replacing, want 600", perm)
	}
	// Nothing left behind: these directories are read by other commands, and a
	// litter of dotfiles in the runtime directory is somebody's bug report.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("%d files in the directory, want just the one", len(entries))
	}
}
