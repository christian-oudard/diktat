// Toggle: tell the running daemon to start or stop recording.
package main

import (
	"log"
	"syscall"

	"github.com/christian-oudard/diktat/internal/ipc"
)

func runToggle(args []string) {
	pid := ipc.ReadPID()
	if pid == 0 {
		log.Fatal("no diktat-daemon running")
	}
	if err := syscall.Kill(pid, syscall.SIGUSR1); err != nil {
		log.Fatalf("signal daemon: %v", err)
	}
}
