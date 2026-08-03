// Toggle: send SIGUSR1 to the daemon, or start it if absent.
package main

import (
	"log"
	"os/exec"
	"syscall"

	"github.com/christian-oudard/diktat/internal/ipc"
)

func main() {
	if pid := ipc.ReadPID(); pid != 0 {
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
