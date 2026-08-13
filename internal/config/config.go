// Package config reads ~/.config/diktat/config.toml.
package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the on-disk schema. All fields are optional.
type Config struct {
	// Model the daemon starts on. `diktat model` switches the running daemon
	// without touching this, so a restart comes back to a known model rather
	// than to whatever was last selected.
	Model        string            `toml:"model"`
	PasteMethods map[string]string `toml:"paste_methods"`
	HistoryFile  string            `toml:"history_file"`
	// ModelCacheMB caps what resident models may hold together, in MB. The
	// daemon keeps every model it loads so switching back is instant, which
	// needs a ceiling once the models are large: on a shared laptop GPU the
	// memory is wanted by the desktop too. 0 takes two thirds of the compute
	// device's memory.
	ModelCacheMB int `toml:"model_cache_mb"`
}

// DefaultPath returns the standard config location.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "diktat", "config.toml")
}

// Load parses the config file at path. A missing file returns a zero Config
// and no error, so callers can ignore the absence of user config.
func Load(path string) (*Config, error) {
	var c Config
	_, err := toml.DecodeFile(path, &c)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &c, nil
		}
		return nil, err
	}
	return &c, nil
}
