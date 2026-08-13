package asr

import (
	"testing"

	"github.com/christian-oudard/diktat/internal/models"
)

// models.Spec.Vocab is maintained by hand, because the menu has to answer
// which models take vocabulary hints before any of them are downloaded. This
// checks the claim against the library for whatever is present, so the hand
// list cannot drift from the truth unnoticed.
func TestVocabMatchesLibrary(t *testing.T) {
	checked := 0
	for _, s := range models.Catalog {
		if !s.Downloaded() {
			continue
		}
		m, err := Load(s.Path())
		if err != nil {
			t.Errorf("%s: %v", s.Name, err)
			continue
		}
		if got := m.TakesVocabulary(); got != s.Vocab {
			t.Errorf("%s: catalog says Vocab=%v, the library says %v", s.Name, s.Vocab, got)
		}
		m.Close()
		checked++
	}
	if checked == 0 {
		t.Skip("no models downloaded to check against")
	}
	t.Logf("checked %d downloaded models", checked)
}
