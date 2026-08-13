package config

import (
	"os"
	"path/filepath"
	"strings"
)

// State is what diktat decided, as opposed to what the user wrote. The config
// file is hand-authored, so nothing here rewrites it: losing someone's
// comments and ordering to record a menu choice would be rude, and a file
// that is sometimes yours and sometimes the program's is worse than two
// files.
//
// XDG puts this in XDG_STATE_HOME, "state data that should persist between
// restarts ... that can be reused on a restart". Deleting it costs nothing:
// the daemon falls back to the config file, and then to the built-in default.

// StateDir is where that state lives.
func StateDir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "diktat")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "diktat")
}

// selectedPath holds the model chosen by the last `diktat model`.
func selectedPath() string { return filepath.Join(StateDir(), "model") }

// Selected is the model the user last chose, or "" if they never have. It is
// a menu name or a path, never a menu number, so it survives the menu being
// reordered.
func Selected() string {
	raw, err := os.ReadFile(selectedPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// Select records a model as the one to start with from now on. A failure to
// write is not worth failing the switch over: the model still changes, it
// just will not be remembered, so callers report it and carry on.
func Select(nameOrPath string) error {
	if err := os.MkdirAll(StateDir(), 0755); err != nil {
		return err
	}
	return os.WriteFile(selectedPath(), []byte(nameOrPath+"\n"), 0644)
}
