// diktat is voice dictation for Sway. One binary, one subcommand per job.
package main

import (
	"fmt"
	"os"
)

type command struct {
	name    string
	usage   string
	summary string
	run     func(args []string)
}

var commands = []command{
	{"daemon", "", "Run the Diktat voice transcription daemon.", runDaemon},
	{"toggle", "", "Start or stop recording.", runToggle},
	{"repeat", "", "Repeat the last transcription, typing the text again.", runRepeat},
	{"model", "[<model>]", "List, switch, or fetch voice transcription models.", runModel},
	{"version", "", "Report the build, and whether the daemon matches it.", runVersion},
}

func main() {
	if len(os.Args) < 2 {
		usage(os.Stdout)
		return
	}
	name := os.Args[1]
	if name == "-h" || name == "--help" || name == "help" {
		usage(os.Stdout)
		return
	}
	for _, c := range commands {
		if c.name == name {
			c.run(os.Args[2:])
			return
		}
	}
	fmt.Fprintf(os.Stderr, "unknown command %q\n\n", name)
	usage(os.Stderr)
	os.Exit(1)
}

func usage(w *os.File) {
	fmt.Fprintln(w, "usage: diktat <command> [args]")
	fmt.Fprintln(w)
	for _, c := range commands {
		fmt.Fprintf(w, "  %-7s %-30s %s\n", c.name, c.usage, c.summary)
	}
}
