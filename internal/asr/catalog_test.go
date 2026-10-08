package asr

import (
	"slices"
	"testing"

	"github.com/christian-oudard/diktat/internal/models"
)

// The menu's languages and features are kept by hand, because it has to
// answer before anything is downloaded. This checks them against the library
// for whatever is present, so the hand-kept lists cannot drift unnoticed.
func TestCatalogMatchesLibrary(t *testing.T) {
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
		checkLangs(t, s, m)
		if got, err := m.Streams(); err != nil {
			t.Errorf("%s: capabilities: %v", s.Name, err)
		} else if got != s.Streams() {
			t.Errorf("%s: catalog says streams=%v, the library says %v", s.Name, s.Streams(), got)
		}
		m.Close()
		checked++
	}
	if checked == 0 {
		t.Skip("no models downloaded to check against")
	}
	t.Logf("checked %d downloaded models", checked)
}

// checkLangs compares the catalog's language claim against what the model
// advertises. A nil Langs is the catalog saying "most of them", which is only
// honest if the model really lists many.
func checkLangs(t *testing.T, s models.Spec, m *Model) {
	t.Helper()
	caps, err := m.Languages()
	if err != nil {
		t.Errorf("%s: capabilities: %v", s.Name, err)
		return
	}
	switch {
	case len(caps) == 0:
		// Nothing advertised: the catalog cannot be checked, only trusted.
		t.Logf("%s advertises no language set; catalog says %q", s.Name, s.Languages())
	case len(s.Langs) == 0:
		if len(caps) < 10 {
			t.Errorf("%s: catalog says Worldwide, the library lists only %d: %v", s.Name, len(caps), caps)
		}
	default:
		want := slices.Clone(s.Langs)
		got := slices.Clone(caps)
		slices.Sort(want)
		slices.Sort(got)
		if !slices.Equal(want, got) {
			t.Errorf("%s: catalog says %v, the library says %v", s.Name, want, got)
		}
	}
}
