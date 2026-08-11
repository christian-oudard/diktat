package asr

import "testing"

// Whisper answers silence with a non-speech marker rather than with nothing.
// The daemon only skips typing when the text is empty, so an unfiltered marker
// gets typed into whatever window has focus. Observed: a silent capture typed
// "[BLANK_AUDIO]", whistling typed "[SOUND]", singing typed "(mumbles)".
// Hence matching by shape rather than by a list of known markers: whisper has
// many and they are not enumerated anywhere.
func TestDropAnnotations(t *testing.T) {
	cases := []struct{ in, want string }{
		{"[BLANK_AUDIO]", ""},
		{" [BLANK_AUDIO] ", ""},
		{"[ Silence ]", ""},
		{"[SOUND]", ""}, // whistling
		{"[NOISE]", ""},
		{"(whistling)", ""},
		{"(mumbles)", ""}, // singing
		{"(singing)", ""},
		{"(wind blowing)", ""},
		{"♪", ""},
		{"[MUSIC] ♪ [BLANK_AUDIO]", ""},
		{"", ""},
		// Real speech survives, including alongside an aside.
		{"hello there", "hello there"},
		{"[BLANK_AUDIO] hello there", "hello there"},
		{"hello (coughs) there", "hello there"},
		{"  hello   there  ", "hello there"},
		// Brackets that are part of speech are rare enough to lose; what
		// matters is that nothing crashes and nothing is left dangling.
		{"a [b] c", "a c"},
	}
	for _, c := range cases {
		if got := dropAnnotations(c.in); got != c.want {
			t.Errorf("dropAnnotations(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
