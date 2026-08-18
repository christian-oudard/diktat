package asr

import (
	"strings"
	"testing"
)

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

// The library's own account of a failed load is the only one there is: the
// API answers "gguf load error" and nothing about which part of the file it
// could not read. Its narration is kept off stderr but not thrown away, and a
// failing load quotes what it said. The mark counts every line ever kept
// rather than indexing the ring, since the ring rolls.
func TestComplaintsSinceMark(t *testing.T) {
	complaints.lines, complaints.seen = nil, 0

	mark := complaintMark()
	if got := since(mark); got != "" {
		t.Errorf("a quiet load reported %q, want nothing", got)
	}

	noteComplaint("  gguf: tensor count mismatch  ")
	noteComplaint("gguf: unknown architecture")
	if got, want := since(mark), ": gguf: tensor count mismatch; gguf: unknown architecture"; got != want {
		t.Errorf("since(mark) = %q, want %q", got, want)
	}

	// A load that says nothing after one that said plenty reports nothing,
	// rather than repeating the last load's complaints.
	if got := since(complaintMark()); got != "" {
		t.Errorf("a quiet load inherited %q", got)
	}

	// More lines than the ring holds: what is left is the last few, not
	// silence, which is what indexing the ring by a stale length would give.
	for i := 0; i < keptComplaints*2; i++ {
		noteComplaint("line")
	}
	got := since(mark)
	if n := strings.Count(got, "line"); n != keptComplaints {
		t.Errorf("kept %d lines of %d, want the last %d: %q", n, keptComplaints*2, keptComplaints, got)
	}
}
