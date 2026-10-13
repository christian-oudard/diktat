// Package flagset is the standard flag package with its help spelled the GNU
// way: two dashes for a long option, one for a single letter, and a letter
// that abbreviates a long option on the same line as it. Parsing already
// takes either spelling; only the help wrote one dash for everything.
package flagset

import (
	"flag"
	"fmt"
	"strings"
)

// shorthand is the usage that marks a single letter as another name for a
// long option.
const shorthand = "shorthand for --"

// New is flag.NewFlagSet with ExitOnError and the help above.
func New(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.Usage = func() {
		short := map[string]string{} // long name to its letter
		fs.VisitAll(func(f *flag.Flag) {
			if long, ok := strings.CutPrefix(f.Usage, shorthand); ok {
				short[long] = f.Name
			}
		})
		w := fs.Output()
		fmt.Fprintf(w, "Usage of %s:\n", name)
		fs.VisitAll(func(f *flag.Flag) {
			if strings.HasPrefix(f.Usage, shorthand) {
				return
			}
			kind, usage := flag.UnquoteUsage(f)
			line := "  "
			switch {
			case len([]rune(f.Name)) == 1:
				line += "-" + f.Name
			case short[f.Name] != "":
				line += "-" + short[f.Name] + ", --" + f.Name
			default:
				line += "    --" + f.Name
			}
			if kind != "" {
				line += " " + kind
			}
			line += "\n    \t" + strings.ReplaceAll(usage, "\n", "\n    \t")
			switch f.DefValue {
			case "", "false", "0", "0s":
			default:
				if kind == "string" {
					line += fmt.Sprintf(" (default %q)", f.DefValue)
				} else {
					line += fmt.Sprintf(" (default %s)", f.DefValue)
				}
			}
			fmt.Fprintln(w, line)
		})
	}
	return fs
}
