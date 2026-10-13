package phrase

import (
	"testing"
	"time"

	"github.com/christian-oudard/diktat/internal/silence"
)

func s(sec float64) time.Duration { return time.Duration(sec * float64(time.Second)) }

func TestEnd(t *testing.T) {
	for _, c := range []struct {
		what   string
		speech []silence.Span
		length float64
		want   float64
	}{
		{"nothing said", nil, 10, 0},
		{"still talking", []silence.Span{{Start: s(0.5), End: s(6)}}, 6, 0},
		{"a pause too short", []silence.Span{{Start: s(0.5), End: s(5)}}, 5.5, 0},
		{"a pause after a phrase", []silence.Span{{Start: s(0.5), End: s(5)}}, 6, 5.5},
		{"a phrase too short", []silence.Span{{Start: s(0), End: s(2)}}, 3, 0},
		{"the latest pause", []silence.Span{{Start: s(0), End: s(5)}, {Start: s(6), End: s(10)}, {Start: s(11), End: s(12)}}, 12.1, 10.5},
		{"skips a pause too early", []silence.Span{{Start: s(0), End: s(2)}, {Start: s(3), End: s(6)}, {Start: s(7), End: s(9)}}, 9.2, 6.5},
		{"a pause between short words", []silence.Span{{Start: s(0), End: s(4)}, {Start: s(4.3), End: s(6)}}, 6.2, 0},
	} {
		if got := end(c.speech, s(c.length)); got != s(c.want) {
			t.Errorf("%s: end = %v, want %v", c.what, got, s(c.want))
		}
	}
}
