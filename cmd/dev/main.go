// Command dev measures and inspects models: replaying recordings through the
// dictation path, reading what a model can do out of its file, and running a
// diarizer alone. None of it is part of dictating, so it is kept out of the
// installed diktat; the flake builds only cmd/diktat, which leaves this to
// `go run ./cmd/dev` inside the devShell. It lives in Go rather than in
// scripts because it has to run the real pipeline. See docs/dev.md.
package main

import (
	"fmt"
	"log"
	"os"
)

var commands = []struct {
	name, usage string
	run         func([]string)
}{
	{"replay", "[--model <name>]... [--mode auto|whole|phrases|stream] [--last N] [recording...]", runReplay},
	{"caps", "[model...]", runCaps},
	{"diarize", "[--model <name>] [--preset <point>] [-o rows.tsv] recording", runDiarize},
}

func main() {
	log.SetFlags(0)
	if len(os.Args) > 1 {
		for _, c := range commands {
			if c.name == os.Args[1] {
				c.run(os.Args[2:])
				return
			}
		}
	}
	fmt.Fprintln(os.Stderr, "usage: go run ./cmd/dev <command>; see docs/dev.md")
	for _, c := range commands {
		fmt.Fprintf(os.Stderr, "  %s %s\n", c.name, c.usage)
	}
	os.Exit(2)
}
