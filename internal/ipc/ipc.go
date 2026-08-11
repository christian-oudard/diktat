// Package ipc holds the files the diktat commands use to find each other.
package ipc

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	PIDFile      = "/tmp/diktat-daemon.pid"
	StatusFile   = "/tmp/diktat-status"
	LastTextFile = "/tmp/diktat-last"
	LogFile      = "/tmp/diktat-daemon.log"

	// The audio of the last capture, exactly as handed to the model, so an
	// utterance that transcribed badly can be replayed through
	// diktat-transcribe instead of being re-recorded.
	LastAudioFile = "/tmp/diktat-last.wav"

	// The model directory the daemon currently has loaded. The daemon writes
	// it; diktat-model reads it to report and rewrites it to switch.
	ModelFile = "/tmp/diktat-model"
)

// ModelsDir holds downloaded models that a bare name resolves against, so
// `diktat-model moonshine-base` works without typing a path.
func ModelsDir() string {
	if cache := os.Getenv("XDG_CACHE_HOME"); cache != "" {
		return filepath.Join(cache, "diktat", "models")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "diktat", "models")
}

// ResolveModel turns a bare name into a path under ModelsDir, and leaves
// anything that looks like a path alone.
func ResolveModel(nameOrPath string) string {
	if strings.ContainsRune(nameOrPath, filepath.Separator) || nameOrPath == "." {
		abs, err := filepath.Abs(nameOrPath)
		if err != nil {
			return nameOrPath
		}
		return abs
	}
	path := filepath.Join(ModelsDir(), nameOrPath)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	// Moonshine models are directories, whisper models a single ggml file, so
	// a bare whisper name needs the suffix filling in.
	if bin := path + ".bin"; fileExists(bin) {
		return bin
	}
	// The model the build ships lives in the nix store rather than the cache,
	// but should still answer to its own name: MOONSHINE_MODEL_DIR is a store
	// path like /nix/store/<hash>-moonshine-tiny-models.
	if b := BundledModel(); b != "" && strings.Contains(filepath.Base(b), nameOrPath) {
		return b
	}
	return path
}

// BundledModel is the model the nix wrapper points the binaries at.
func BundledModel() string {
	return os.Getenv("MOONSHINE_MODEL_DIR")
}

// AvailableModels lists every name a bare argument could resolve to, for
// telling the user what they can switch to when a name does not resolve.
func AvailableModels() []string {
	var out []string
	if b := BundledModel(); b != "" {
		out = append(out, fmt.Sprintf("%-18s %s", bundledName(b), b))
	}
	entries, err := os.ReadDir(ModelsDir())
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".bin")
		out = append(out, fmt.Sprintf("%-18s %s", name, filepath.Join(ModelsDir(), e.Name())))
	}
	return out
}

// bundledName strips the store hash and the derivation's -models suffix, so
// /nix/store/<hash>-moonshine-tiny-models reads as moonshine-tiny.
func bundledName(storePath string) string {
	base := filepath.Base(storePath)
	if _, rest, found := strings.Cut(base, "-"); found {
		base = rest
	}
	return strings.TrimSuffix(base, "-models")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// CheckModel reports whether path looks like something the daemon can load:
// a directory of moonshine ONNX files, or a whisper ggml .bin.
func CheckModel(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if !strings.HasSuffix(path, ".bin") {
			return fmt.Errorf("%s: not a whisper .bin", path)
		}
		return nil
	}
	for _, f := range []string{"encoder.onnx", "decoder.onnx", "tokenizer.json"} {
		if _, err := os.Stat(filepath.Join(path, f)); err != nil {
			return fmt.Errorf("%s is not a moonshine model directory: missing %s", path, f)
		}
	}
	return nil
}

// ReadPID returns the live daemon's PID, or 0 if there is none.
func ReadPID() int {
	return readPID(PIDFile)
}

func readPID(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0
	}
	// Probe liveness with signal 0.
	if syscall.Kill(pid, 0) != nil {
		return 0
	}
	return pid
}

// ExePath returns the binary a process is running. Under nix this is the
// store path, which is what identifies the build.
func ExePath(pid int) string {
	path, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return ""
	}
	return path
}
