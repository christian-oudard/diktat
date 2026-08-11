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
	// debug commands work but stay out of the usage listing; they are for
	// diagnosing a microphone, not for dictating.
	debug bool
}

var commands = []command{
	{"daemon", "", "Run the Diktat voice transcription daemon.", runDaemon, false},
	{"toggle", "", "Start or stop recording.", runToggle, false},
	{"repeat", "", "Repeat the last transcription, typing the text again.", runRepeat, false},
	{"model", "[<model> | download <model>]", "Manage voice transcription models.", runModel, false},
	{"version", "", "Report the build, and whether the daemon matches it.", runVersion, false},
	{"record", "[out.wav]", "Capture a WAV from the mic, with a level meter.", runRecord, true},
	{"transcribe", "[-raw] <wav>...", "Transcribe WAV files offline.", runTranscribe, true},
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
		if c.debug {
			continue
		}
		fmt.Fprintf(w, "  %-7s %-30s %s\n", c.name, c.usage, c.summary)
	}
}
