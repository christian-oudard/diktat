package main

import (
	"bytes"
	"strings"
	"testing"
)

// A terminal width columns wide, with the meter and the text on one screen,
// and twelve frames of audio: sound for five, silence for one, sound for four,
// silence for two. Any sound draws full and silence blank.
func testBoard(width int) (*board, *bytes.Buffer) {
	var screen bytes.Buffer
	b := &board{meter: &screen, text: &screen, width: func() int { return width },
		glyph: func(f []int16) rune {
			for _, s := range f {
				if s != 0 {
					return '█'
				}
			}
			return ' '
		}}
	audio := make([]int16, 12*frame)
	for i := range audio {
		if i < 5*frame || i >= 6*frame && i < 10*frame {
			audio[i] = 1000
		}
	}
	b.add(audio)
	return b, &screen
}

func blocks(n int) string { return strings.Repeat("▄", n) }

var (
	hello = stamped{"hello", 0, 5 * frame}
	there = stamped{"there", 6 * frame, 10 * frame}
)

// A word replaces the blocks of its audio, and the blocks after it stay below.
// The next word joins it on the same row, which is drawn again.
func TestBoardGrowsTheRow(t *testing.T) {
	b, screen := testBoard(20)
	b.Write([]byte(blocks(12)))
	b.show([]stamped{hello})
	want := blocks(12) + "\r\x1b[J" + "█████\n" + "hello\n" + blocks(7)
	if screen.String() != want {
		t.Errorf("got %q\nwant %q", screen.String(), want)
	}

	screen.Reset()
	b.show([]stamped{hello, there})
	want = "\x1b[2A\r\x1b[J" + "█████ █████\n" + "hello there\n" + blocks(2)
	if screen.String() != want {
		t.Errorf("got %q\nwant %q", screen.String(), want)
	}
}

// A word that does not fit finishes the row, which is never drawn again.
func TestBoardFinishesAFullRow(t *testing.T) {
	b, screen := testBoard(10)
	b.Write([]byte(blocks(12)))
	b.show([]stamped{hello, there})
	want := blocks(12) + "\x1b[1A\r\x1b[J" + "█████\n" + "hello\n" + "█████\n" + "there\n" + blocks(2)
	if screen.String() != want {
		t.Errorf("got %q\nwant %q", screen.String(), want)
	}

	screen.Reset()
	b.show([]stamped{hello, there})
	want = "\x1b[2A\r\x1b[J" + "█████\n" + "there\n" + blocks(2)
	if screen.String() != want {
		t.Errorf("got %q\nwant %q", screen.String(), want)
	}
}

// A row filled to the last column leaves the cursor on it, so a full row of
// blocks is one row to go back over, not two.
func TestBoardFullRowOfBlocks(t *testing.T) {
	b, screen := testBoard(10)
	b.Write([]byte(blocks(10)))
	b.show([]stamped{hello})
	want := blocks(10) + "\r\x1b[J" + "█████\n" + "hello\n" + blocks(5)
	if screen.String() != want {
		t.Errorf("got %q\nwant %q", screen.String(), want)
	}
}

// The meter's last row is ended when recording stops, and only if it has
// anything on it.
func TestBoardClose(t *testing.T) {
	b, screen := testBoard(10)
	b.close()
	b.Write([]byte(blocks(3)))
	b.close()
	if want := blocks(3) + "\n"; screen.String() != want {
		t.Errorf("got %q, want %q", screen.String(), want)
	}
}

// heardIn draws full where a range touches speech and blank where it does not.
func heardIn(spans ...[2]int) func(from, to int) rune {
	return func(from, to int) rune {
		for _, s := range spans {
			if from < s[1] && to > s[0] {
				return '█'
			}
		}
		return ' '
	}
}

// fitted lays out words fitted to their sound, as the board does.
func fitted(words []stamped, width int, glyph func(from, to int) rune) []row {
	return wordRows(audible(words, 0, glyph), width, glyph)
}

// Letters take their word's audio; the punctuation and space between two
// words take the silence between them.
func TestWordRowsPutSilenceBetweenWords(t *testing.T) {
	words := []stamped{{"Hello,", 0, 1000}, {"world.", 3000, 4000}}
	rows := fitted(words, 80, heardIn([2]int{0, 1000}, [2]int{3000, 4000}))
	if len(rows) != 1 || rows[0].levels != "█████  ██████" || rows[0].text != "Hello, world." {
		t.Errorf("got %q", rows)
	}
}

// Words that run together leave no silence, so the space between them draws
// the sound either side of it.
func TestWordRowsRunTogether(t *testing.T) {
	words := []stamped{{"ab", 0, 1000}, {"cd", 1000, 2000}}
	rows := fitted(words, 80, heardIn([2]int{0, 2000}))
	if len(rows) != 1 || rows[0].levels != "█████" {
		t.Errorf("got %q, want the space drawn", rows)
	}
}

func TestWordRowsWrap(t *testing.T) {
	words := []stamped{{"aaaa", 0, 100}, {"bbbb", 200, 300}, {"cccc", 400, 500}}
	rows := fitted(words, 10, heardIn())
	if len(rows) != 2 || rows[0].text != "aaaa bbbb" || rows[0].words != 2 ||
		rows[1].text != "cccc" || rows[1].words != 1 {
		t.Errorf("got %+v, want two rows of whole words", rows)
	}
	for _, r := range rows {
		if len([]rune(r.levels)) != len([]rune(r.text)) {
			t.Errorf("levels %q are not as wide as %q", r.levels, r.text)
		}
	}
}

// A word is stamped as lasting through the pause after it, so the pause is
// taken back out of it by sound, and draws in the gap where the text pauses.
func TestWordRowsTrimSilenceOffWords(t *testing.T) {
	words := []stamped{{"ab", 0, 3000}, {"cd", 3000, 4000}}
	rows := fitted(words, 80, heardIn([2]int{0, 1000}, [2]int{3000, 4000}))
	if len(rows) != 1 || rows[0].levels != "██ ██" {
		t.Errorf("got %q, want the pause in the gap", rows)
	}
}

// A word with no sound in it at all keeps its times.
func TestWordRowsKeepASilentWord(t *testing.T) {
	words := []stamped{{"ab", 1000, 2000}}
	rows := fitted(words, 80, heardIn([2]int{0, 500}, [2]int{2500, 3000}))
	if len(rows) != 1 || rows[0].levels != "  " {
		t.Errorf("got %q", rows)
	}
}

// A word can also be stamped as ending before its sound does, so it is
// grown over the sound beside it, up to the next word's stamp.
func TestWordRowsGrowWordsOverTheirSound(t *testing.T) {
	words := []stamped{{"ab", 0, 640}, {"cd", 3200, 3840}}
	rows := fitted(words, 80, heardIn([2]int{0, 1920}, [2]int{2560, 3840}))
	if len(rows) != 1 || rows[0].levels != "██ ██" {
		t.Errorf("got %q, want the sound on the letters", rows)
	}
}

// The first word of a row grows back no further than where the word before
// it, on the row above, ended.
func TestAudibleStopsAtTheWordBefore(t *testing.T) {
	ws := audible([]stamped{{"cd", 3200, 3840}}, 1920, heardIn([2]int{0, 3840}))
	if ws[0].start != 1920 {
		t.Errorf("starts at %d, want 1920", ws[0].start)
	}
}
