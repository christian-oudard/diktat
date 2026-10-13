package flagset

import (
	"bytes"
	"testing"
	"time"
)

// Long options are written with two dashes, and a single letter declared as
// "shorthand for --name" shares its long option's line rather than having one
// of its own.
func TestHelp(t *testing.T) {
	fs := New("transcribe")
	var out string
	fs.StringVar(&out, "output", "", "write the transcript to this `file`")
	fs.StringVar(&out, "o", "", "shorthand for --output")
	fs.Bool("timestamps", false, "put the time of each turn in the document")
	fs.Duration("wait", time.Second, "how long to wait")
	fs.Int("n", 0, "a letter with no long form")
	var b bytes.Buffer
	fs.SetOutput(&b)
	fs.Usage()
	want := `Usage of transcribe:
  -n int
    	a letter with no long form
  -o, --output file
    	write the transcript to this file
      --timestamps
    	put the time of each turn in the document
      --wait duration
    	how long to wait (default 1s)
`
	if b.String() != want {
		t.Errorf("got\n%s\nwant\n%s", b.String(), want)
	}
}
