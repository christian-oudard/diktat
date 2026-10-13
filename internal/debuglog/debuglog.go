// Package debuglog is where debug mode keeps each dictation and the line it
// writes about it, shared by the daemon that writes them and `dev replay`
// that reads them back. See docs/debug.md.
package debuglog

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/christian-oudard/diktat/internal/xdg"
)

// Dir holds the kept audio and the log. Only its owner can read it, since
// both are what somebody said.
func Dir() string { return filepath.Join(xdg.CacheDir(), "debug") }

const logName = "dictations.jsonl"

// Entry is one dictation's line in the log.
type Entry struct {
	Audio   string    `json:"audio"` // file name in Dir
	Model   string    `json:"model"`
	Seconds float64   `json:"seconds"`
	Text    string    `json:"text"`
	Phrases []Stretch `json:"phrases"`
	WaitMS  *Wait     `json:"wait_ms"` // nil when nothing was typed
}

// Stretch is one stretch of a dictation and what the model made of it.
type Stretch struct {
	Start  float64 `json:"start"` // seconds into the recording
	End    float64 `json:"end"`
	TookMS int64   `json:"took_ms"`
	Text   string  `json:"text"`
}

// Wait is how long a dictation took to type after the key stopped it, in
// milliseconds, and where that went.
type Wait struct {
	Total int64 `json:"total"`
	Model int64 `json:"model"` // finishing what the model was running
	Last  int64 `json:"last"`  // transcribing what the model had not yet heard
	Type  int64 `json:"type"`
}

// Append adds e to the log.
func Append(e Entry) error {
	if err := os.MkdirAll(Dir(), 0700); err != nil {
		return err
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(Dir(), logName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Read is every entry in the log, oldest first, or none if nothing was kept.
func Read() ([]Entry, error) {
	f, err := os.Open(filepath.Join(Dir(), logName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var entries []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<24)
	for sc.Scan() {
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, sc.Err()
}
