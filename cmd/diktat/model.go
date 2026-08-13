package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"syscall"

	"github.com/christian-oudard/diktat/internal/config"
	"github.com/christian-oudard/diktat/internal/ipc"
	"github.com/christian-oudard/diktat/internal/models"
)

// runModel: bare lists the menu and offers to switch, anything else switches
// to that model, fetching it first if it is not in the cache yet.
func runModel(args []string) {
	log.SetFlags(0)
	if len(args) > 0 {
		switchModel(args[0])
		return
	}

	inUse := listModels()

	// Only offer the choice when the listing is being read by a person.
	// Piped, this command is how the zsh completion learns the menu, and a
	// prompt there would hang the shell waiting for an answer nobody is
	// there to give.
	if !terminal(os.Stdout) {
		return
	}
	if choice := askWhich(inUse); choice != "" {
		switchModel(choice)
	}
}

// askWhich offers the menu numbers, with an empty answer meaning "leave it
// alone" so the listing stays usable as a listing.
func askWhich(inUse string) string {
	keep := "change nothing"
	if inUse != "" {
		keep = "keep " + inUse
	}
	fmt.Printf("\nSelect 1-%d, or Enter to %s: ", len(models.Catalog), keep)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		fmt.Println()
		return ""
	}
	return strings.TrimSpace(line)
}

// listModels numbers the menu, since the names are long and switching by
// hand is the common case. The name stays in a fixed column so completion
// can read it. It returns the menu name of the model in use, or "".
func listModels() string {
	loaded := loadedModel()
	inUse := ""
	for i, s := range models.Catalog {
		state, mark := "not downloaded", " "
		if s.Downloaded() {
			state = "downloaded"
		}
		if loaded != "" && s.Path() == loaded {
			state, mark, inUse = "downloaded, in-use", "*", s.Name
		}
		vocab := "     "
		if s.Vocab {
			vocab = "vocab"
		}
		fmt.Printf("%s %d %-28s %5d MB  %s  %s\n", mark, i+1, s.Name, s.MB, vocab, state)
	}
	// Whether a daemon is up no longer changes what choosing a model does, so
	// its absence is not worth reporting. A daemon on a path outside the menu
	// still is: there would otherwise be no marker anywhere and no way to
	// tell what is loaded.
	if loaded != "" && inUse == "" {
		fmt.Printf("\nloaded: %s\n", loaded)
	}
	return inUse
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

	// Remember the choice before acting on it, so it holds whether or not a
	// daemon is up to hear about it. What gets recorded is the menu name
	// where there is one, never the menu number, which would mean something
	// else if the menu were reordered.
	remembered := nameOrNumber
	if inMenu {
		remembered = spec.Name
	}
	if err := config.Select(remembered); err != nil {
		log.Printf("could not remember the choice: %v", err)
	}

	pid := ipc.ReadPID()
	if pid == 0 {
		// Choosing a model with no daemon running is a reasonable thing to
		// want; it is how you set up the next one.
		fmt.Printf("%s is ready, and is what the daemon will start on\n", remembered)
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

// terminal reports whether f is a character device, which is what makes a
// prompt worth printing. A pipe or a file is something reading the output,
// not someone answering it.
func terminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
