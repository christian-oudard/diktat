package models

import "testing"

// The catalog claims a language set per model so the menu can answer before
// anything is downloaded. asr.TestLangsMatchLibrary checks those claims
// against the library; this only checks the rendering, which the menu depends
// on for its column width.
func TestLanguagesRendering(t *testing.T) {
	for _, c := range []struct {
		langs []string
		want  string
	}{
		{nil, "all"},
		{[]string{}, "all"},
		{[]string{"en"}, "en"},
		{[]string{"en", "de", "es", "fr"}, "en de es fr"},
	} {
		if got := (Spec{Langs: c.langs}).Languages(); got != c.want {
			t.Errorf("Languages(%v) = %q, want %q", c.langs, got, c.want)
		}
	}
	// The column is padded to 14; anything longer would push the state column
	// out of line for every row.
	for _, s := range Catalog {
		if got := s.Languages(); len(got) > 14 {
			t.Errorf("%s: language column %q is %d wide, over the 14 the menu pads to",
				s.Name, got, len(got))
		}
	}
}
