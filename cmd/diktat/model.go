package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"syscall"

	"github.com/christian-oudard/diktat/internal/ipc"
	"github.com/christian-oudard/diktat/internal/models"
)

// runModel: bare lists the menu, anything else switches to that model,
// fetching it first if it is not in the cache yet.
func runModel(args []string) {
	log.SetFlags(0)
	if len(args) == 0 {
		listModels()
		return
	}
	switchModel(args[0])
}

// listModels numbers the menu, since the names are long and switching by
// hand is the common case. The name stays in a fixed column so completion
// can read it.
func listModels() {
	loaded := loadedModel()
	matched := false
	for i, s := range models.Catalog {
		state, mark := "not downloaded", " "
		if s.Downloaded() {
			state = "downloaded"
		}
		if loaded != "" && s.Path() == loaded {
			state, mark, matched = "downloaded, in-use", "*", true
		}
		fmt.Printf("%s %d %-28s %5d MB  %s\n", mark, i+1, s.Name, s.MB, state)
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

// switchModel points the daemon at a model, given its menu number, its name,
// or a path. A menu entry that is not in the cache is offered for download
// rather than refused, since wanting to use a model is the only reason to
// name one.
func switchModel(nameOrNumber string) {
	path := models.Resolve(nameOrNumber)
	spec, inMenu := models.Lookup(nameOrNumber)

	// Sort the model out before the daemon: a typo is the likelier mistake,
	// and it is fixable without starting anything.
	if err := models.Check(path); err != nil {
		if !inMenu {
			log.Fatalf("unknown model %q; the menu is: diktat model", nameOrNumber)
		}
		if !confirm(fmt.Sprintf("%s is not downloaded. Fetch it now (%d MB)?", spec.Name, spec.MB)) {
			log.Fatal("cancelled")
		}
		p, err := models.Download(spec.Name, os.Stderr)
		if err != nil {
			log.Fatal(err)
		}
		path = p
	}

	pid := ipc.ReadPID()
	if pid == 0 {
		// Downloading without a daemon running is a reasonable thing to
		// want, so report the model rather than failing after fetching it.
		fmt.Printf("%s is ready; no daemon running\n", path)
		return
	}
	if err := os.WriteFile(ipc.ModelFile, []byte(path), 0644); err != nil {
		log.Fatalf("write model file: %v", err)
	}
	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		log.Fatalf("signal daemon: %v", err)
	}
	fmt.Printf("switching to %s (watch %s)\n", path, ipc.LogFile)
}

// confirm asks before spending someone's bandwidth, since a model runs to a
// couple of gigabytes. Yes is the default, so nothing to read counts as yes:
// stdin closed or coming from /dev/null means nobody is there to answer, and
// naming a model is intent enough on its own.
func confirm(question string) bool {
	fmt.Fprintf(os.Stderr, "%s [Y/n] ", question)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		fmt.Fprintln(os.Stderr, "y")
		return true
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "", "y", "yes":
		return true
	}
	return false
}
