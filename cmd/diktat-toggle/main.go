// Toggle: send SIGUSR1 to the daemon, or start it if absent.
package main

import (
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

const pidFile = "/tmp/diktat-daemon.pid"

func main() {
	if pid := readPID(); pid != 0 {
		if err := syscall.Kill(pid, syscall.SIGUSR1); err == nil {
			return
		}
	}
	// No live daemon. Start one detached.
	cmd := exec.Command("diktat-daemon")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		log.Fatalf("start daemon: %v", err)
	}
	_ = cmd.Process.Release()
}

func readPID() int {
	raw, err := os.ReadFile(pidFile)
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
