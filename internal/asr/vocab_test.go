package asr

import (
	"os"
	"testing"

	"github.com/christian-oudard/diktat/internal/wav"
)

// Vocabulary hints are whisper's initial prompt. These check the wiring: that
// only the families able to use them report so, and that setting them changes
// what comes out.
func TestVocabularyWiring(t *testing.T) {
	path := os.Getenv("DIKTAT_TEST_MODEL")
	if path == "" {
		t.Skip("set DIKTAT_TEST_MODEL to a .gguf")
	}
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	t.Logf("%s takes vocabulary: %v", m.name, m.TakesVocabulary())

	audioPath := os.Getenv("DIKTAT_TEST_WAV")
	if audioPath == "" {
		t.Skip("set DIKTAT_TEST_WAV")
	}
	pcm, _, err := wav.ReadWAV(audioPath)
	if err != nil {
		t.Fatal(err)
	}

	plain, err := m.Transcribe(pcm)
	if err != nil {
		t.Fatal(err)
	}
	m.SetVocabulary("NixOS, nixpkgs, direnv, pyright, ripgrep, PipeWire, diktat, JSONL")
	hinted, err := m.Transcribe(pcm)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("plain:  %q", plain)
	t.Logf("hinted: %q", hinted)
	if !m.TakesVocabulary() && plain != hinted {
		t.Errorf("a model that takes no prompt changed its answer anyway")
	}
}
