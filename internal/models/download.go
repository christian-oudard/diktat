package models

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const (
	moonshineHF = "https://huggingface.co/UsefulSensors"
	whisperHF   = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main"
)

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

	if spec.Kind == Whisper {
		dest := spec.Path()
		url := fmt.Sprintf("%s/ggml-%s.bin", whisperHF, spec.size)
		return dest, get(url, dest, progress)
	}

	dir := spec.Path()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	onnx := fmt.Sprintf("%s/moonshine/resolve/main/onnx/merged/%s/float", moonshineHF, spec.size)
	files := []struct{ url, dest string }{
		{onnx + "/encoder_model.onnx", filepath.Join(dir, "encoder.onnx")},
		{onnx + "/decoder_model_merged.onnx", filepath.Join(dir, "decoder.onnx")},
		{fmt.Sprintf("%s/moonshine-%s/resolve/main/tokenizer.json", moonshineHF, spec.size),
			filepath.Join(dir, "tokenizer.json")},
	}
	for _, f := range files {
		if err := get(f.url, f.dest, progress); err != nil {
			return "", err
		}
	}
	return dir, nil
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
