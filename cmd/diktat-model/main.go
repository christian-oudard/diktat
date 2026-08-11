// Model: report or switch the model the running daemon has loaded, without
// restarting it or losing the session.
//
//	diktat-model                   print the loaded model
//	diktat-model moonshine-base    switch, resolving under the models cache
//	diktat-model ./some/dir        switch to an explicit path
package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"

	"github.com/christian-oudard/diktat/internal/ipc"
)

func main() {
	log.SetFlags(0)
	pid := ipc.ReadPID()
	if pid == 0 {
		log.Fatal("no diktat-daemon running")
	}

	if len(os.Args) < 2 {
		raw, err := os.ReadFile(ipc.ModelFile)
		if err != nil {
			log.Fatalf("read current model: %v", err)
		}
		fmt.Println(strings.TrimSpace(string(raw)))
		return
	}

	dir := ipc.ResolveModel(os.Args[1])
	if err := ipc.CheckModel(dir); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(ipc.ModelFile, []byte(dir), 0644); err != nil {
		log.Fatalf("write model file: %v", err)
	}
	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		log.Fatalf("signal daemon: %v", err)
	}
	fmt.Printf("switching to %s (watch %s)\n", dir, ipc.LogFile)
}
