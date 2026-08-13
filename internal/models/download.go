package models

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// hfOrg publishes the GGUF conversion of every model in the menu, one repo
// per model named after it.
const hfOrg = "https://huggingface.co/handy-computer"

// Download fetches a menu entry into the cache and returns where it landed.
// Files already present are left alone, so re-running is cheap. Downloads are
// never implicit: something has to ask for this by name.
func Download(name string, progress io.Writer) (string, error) {
	spec, ok := Lookup(name)
	if !ok {
		return "", fmt.Errorf("unknown model %q", name)
	}
	if err := os.MkdirAll(Dir(), 0755); err != nil {
		return "", err
	}
	dest := spec.Path()
	url := fmt.Sprintf("%s/%s-gguf/resolve/main/%s", hfOrg, spec.Name, spec.File())
	return dest, get(url, dest, progress)
}

func get(url, dest string, progress io.Writer) error {
	if info, err := os.Stat(dest); err == nil && info.Size() > 0 {
		fmt.Fprintf(progress, "have     %s\n", filepath.Base(dest))
		return nil
	}
	fmt.Fprintf(progress, "fetching %s\n", filepath.Base(dest))

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}

	// Write under a temporary name so an interrupted download is not mistaken
	// for a complete one next time.
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}
