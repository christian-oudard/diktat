package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"syscall"

	"github.com/christian-oudard/diktat/internal/ipc"
	"github.com/christian-oudard/diktat/internal/models"
)

// runModel: bare lists the menu, a name switches to it, download fetches.
func runModel(args []string) {
	log.SetFlags(0)
	switch {
	case len(args) == 0:
		listModels()
	case args[0] == "download":
		downloadModel(args[1:])
	default:
		switchModel(args[0])
	}
}

// listModels keeps the name in the first column so completion can read it.
func listModels() {
	loaded := loadedModel()
	matched := false
	for _, s := range models.Catalog {
		state, mark := "not downloaded", " "
		if s.Downloaded() {
			state = "downloaded"
		}
		if loaded != "" && s.Path() == loaded {
			state, mark, matched = "downloaded, in-use", "*", true
		}
		fmt.Printf("%s %-28s %5d MB  %s\n", mark, s.Name, s.MB, state)
	}
	switch {
	case loaded == "":
		fmt.Println("\nno daemon running")
	case !matched:
		// A daemon can be on a path outside the menu, including one from an
		// older build. Say so rather than showing no marker at all.
		fmt.Printf("\nloaded: %s\n", loaded)
	}
}

// loadedModel is what the running daemon has, or "" if none is running.
func loadedModel() string {
	if ipc.ReadPID() == 0 {
		return ""
	}
	raw, err := os.ReadFile(ipc.ModelFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func downloadModel(args []string) {
	if len(args) == 0 {
		log.Fatal("usage: diktat model download <model>; the menu is: diktat model")
	}
	path, err := models.Download(args[0], os.Stderr)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(path)
}

func switchModel(name string) {
	// Complain about the name before the daemon: a typo is the likelier
	// mistake, and it is fixable without starting anything.
	path := models.Resolve(name)
	if err := models.Check(path); err != nil {
		if s, ok := models.Lookup(name); ok {
			log.Fatalf("%s is not downloaded. Get it with:\n  diktat model download %s", s.Name, s.Name)
		}
		log.Fatalf("unknown model %q; the menu is: diktat model", name)
	}
	pid := ipc.ReadPID()
	if pid == 0 {
		log.Fatal("no daemon running")
	}
	if err := os.WriteFile(ipc.ModelFile, []byte(path), 0644); err != nil {
		log.Fatalf("write model file: %v", err)
	}
	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		log.Fatalf("signal daemon: %v", err)
	}
	fmt.Printf("switching to %s (watch %s)\n", path, ipc.LogFile)
}
