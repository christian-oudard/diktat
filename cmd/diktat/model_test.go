package main

import (
	"strings"
	"testing"

	"github.com/christian-oudard/diktat/internal/models"
)

func TestInUseEntryOnMenu(t *testing.T) {
	want := models.Catalog[1]
	got, err := inUseEntry(want.Path())
	if err != nil || got.Name != want.Name {
		t.Errorf("inUseEntry(%q) = %q, %v; want %q", want.Path(), got.Name, err, want.Name)
	}
}

// A model in use that the menu does not know is an error, not a menu with
// nothing marked.
func TestInUseEntryOffMenu(t *testing.T) {
	path := "/elsewhere/some-model.gguf"
	_, err := inUseEntry(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("inUseEntry(%q) error = %v; want one naming the path", path, err)
	}
}
