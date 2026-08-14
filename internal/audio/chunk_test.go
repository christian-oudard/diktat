package audio

import "testing"

// Audio that fits is not worth cutting.
func TestChunkShortAudioIsOnePiece(t *testing.T) {
	s := sine(0.5, 3*SampleRate)
	got := Chunk(s, 10*SampleRate)
	if len(got) != 1 || len(got[0]) != len(s) {
		t.Fatalf("got %d chunks, want the input back whole", len(got))
	}
}

// No limit means no cutting, however long the audio.
func TestChunkNoLimit(t *testing.T) {
	s := sine(0.5, 30*SampleRate)
	if got := Chunk(s, 0); len(got) != 1 {
		t.Errorf("got %d chunks with no limit, want 1", len(got))
	}
}

// Every piece has to be something the model will accept, and nothing may be
// dropped on the floor: what went in is what comes out, in order.
func TestChunkPiecesFitAndKeepEverything(t *testing.T) {
	const max = 5 * SampleRate
	s := sine(0.5, 23*SampleRate)
	chunks := Chunk(s, max)
	total := 0
	for i, c := range chunks {
		if len(c) > max {
			t.Errorf("chunk %d is %d samples, over the %d limit", i, len(c), max)
		}
		if len(c) == 0 {
			t.Errorf("chunk %d is empty", i)
		}
		total += len(c)
	}
	if total != len(s) {
		t.Errorf("chunks hold %d samples, want all %d", total, len(s))
	}
}

// A cut through the middle of a word costs a word. Given a silent gap inside
// the search window, the cut belongs in the gap.
func TestChunkCutsAtSilence(t *testing.T) {
	const max = 10 * SampleRate
	s := sine(0.5, 25*SampleRate)
	// A half-second of silence centred at 9s, inside the window searched for
	// a seam before the 10s limit.
	gap := int(8.75 * SampleRate)
	for i := gap; i < gap+SampleRate/2; i++ {
		s[i] = 0
	}
	first := len(Chunk(s, max)[0])
	if first < gap || first > gap+SampleRate/2 {
		t.Errorf("first chunk ends at sample %d, want the silence at %d..%d",
			first, gap, gap+SampleRate/2)
	}
}
