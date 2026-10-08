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
		{"still talking", []silence.Span{{s(0.5), s(6)}}, 6, 0},
		{"a pause too short", []silence.Span{{s(0.5), s(5)}}, 5.5, 0},
		{"a pause after a phrase", []silence.Span{{s(0.5), s(5)}}, 6, 5.5},
		{"a phrase too short", []silence.Span{{s(0), s(2)}}, 3, 0},
		{"the latest pause", []silence.Span{{s(0), s(5)}, {s(6), s(10)}, {s(11), s(12)}}, 12.1, 10.5},
		{"skips a pause too early", []silence.Span{{s(0), s(2)}, {s(3), s(6)}, {s(7), s(9)}}, 9.2, 6.5},
		{"a pause between short words", []silence.Span{{s(0), s(4)}, {s(4.3), s(6)}}, 6.2, 0},
	} {
		if got := end(c.speech, s(c.length)); got != s(c.want) {
			t.Errorf("%s: end = %v, want %v", c.what, got, s(c.want))
		}
	}
}
