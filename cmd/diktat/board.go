package main

import (
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
	"strings"
	"syscall"
	"unicode"
	"unsafe"

	"github.com/christian-oudard/diktat/internal/audio"
)

// board is the record meter with the words drawn under the levels of their
// own audio. The meter writes its blocks here, one rune per frame of the
// recording, and they go straight to the terminal. The bottom of the screen
// is the row being written, if there is one, and the blocks after its last
// word. When the words change, that area is cleared and drawn again; a row
// the next word does not fit on is finished, drawn above it, and never
// touched again.
type board struct {
	meter, text io.Writer
	width       func() int
	// glyph draws a stretch of audio on the meter's scale.
	glyph func([]int16) rune
	// audio is the recording from sample base on, which is all a row still
	// to be drawn can need.
	audio []int16
	base  int
	// blocks is every block the meter has drawn. Those from shown on are at
	// the bottom, below the row being written if row is set.
	blocks []rune
	shown  int
	row    bool
	// done is how many words are in finished rows, and prev is where the last
	// of them ends.
	done, prev int
}

func (b *board) Write(p []byte) (int, error) {
	b.blocks = append(b.blocks, []rune(string(p))...)
	return b.meter.Write(p)
}

func (b *board) add(samples []int16) { b.audio = append(b.audio, samples...) }

// show draws words, all of the text so far, in place of the bottom area.
func (b *board) show(words []stamped) {
	w := b.width()
	// Back to the top of the area. A row filled to the last column leaves the
	// cursor on it, so the blocks take up ceil(len/width) rows, and at least
	// the one the cursor is on.
	up := max((len(b.blocks)-b.shown+w-1)/w, 1) - 1
	if b.row {
		up += 2
	}
	if up > 0 {
		fmt.Fprintf(b.meter, "\x1b[%dA", up)
	}
	fmt.Fprint(b.meter, "\r\x1b[J")

	sound := func(from, to int) rune {
		at := func(i int) int { return min(max(i-b.base, 0), len(b.audio)) }
		return b.glyph(b.audio[at(from):at(to)])
	}
	fit := audible(words[min(b.done, len(words)):], b.prev, sound)
	rows := wordRows(fit, w, sound)
	for ; len(rows) > 1; rows = rows[1:] {
		b.draw(rows[0])
		b.done += rows[0].words
		b.prev = fit[rows[0].words-1].end
		fit = fit[rows[0].words:]
	}
	end := b.prev
	if b.row = len(rows) == 1; b.row {
		b.draw(rows[0])
		end = fit[len(fit)-1].end
	}
	if cut := b.prev - b.base; cut > 0 {
		b.audio = slices.Clone(b.audio[min(cut, len(b.audio)):])
		b.base = b.prev
	}
	b.shown = min((end+frame/2)/frame, len(b.blocks))
	fmt.Fprint(b.meter, string(b.blocks[b.shown:]))
}

func (b *board) draw(r row) {
	fmt.Fprintln(b.meter, r.levels)
	fmt.Fprintln(b.text, r.text)
}

// close ends the meter's last row.
func (b *board) close() {
	if len(b.blocks) > b.shown {
		fmt.Fprintln(b.meter)
	}
}

// columns is the width of the terminal on fd, or 0 if it is not one.
func columns(fd uintptr) int {
	var ws struct{ rows, cols, x, y uint16 }
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGWINSZ,
		uintptr(unsafe.Pointer(&ws))); errno != 0 {
		return 0
	}
	return int(ws.cols)
}

// stamped is one word and where its audio is, in samples from the start of
// the recording.
type stamped struct {
	text       string
	start, end int
}

// row is one line of words and the levels drawn over it.
type row struct {
	levels, text string
	words        int
}

// wordWindow is the least audio a column is drawn from: a space between words
// that run together stands for no time at all, and draws the sound around
// that moment.
const wordWindow = audio.SampleRate / 50

// wordRows lays words out in rows of at most width columns, with a level over
// every column drawn from the audio that column stands for. A word's letters
// stand for its own audio, stretched or squeezed to their width; the
// punctuation and space between two words' letters stand for the time between
// them, so a pause draws blank where the text pauses.
func wordRows(words []stamped, width int, glyph func(from, to int) rune) []row {
	var rows []row
	for len(words) > 0 {
		n, w := 1, len([]rune(words[0].text))
		for n < len(words) && w+1+len([]rune(words[n].text)) <= width {
			w += 1 + len([]rune(words[n].text))
			n++
		}
		rows = append(rows, wordRow(words[:n], glyph))
		words = words[n:]
	}
	return rows
}

// audible fits each word's times to its sound, by the meter's own test of
// what is silent. The model's stamps can be out by half a second either way,
// and a word stamped as lasting through the pause after it would draw the
// pause on its letters, or one stamped as ending early would draw its tail in
// the gap. So silence is trimmed off a word's ends, and the sound beside it is
// taken in, up to the words either side, or for the first word back to lo,
// where the word before it ended. A word with no sound in it keeps its times.
func audible(words []stamped, lo int, glyph func(from, to int) rune) []stamped {
	out := slices.Clone(words)
	silent := func(from, to int) bool { return glyph(from, to) == heights[0] }
	for i, w := range out {
		for w.start+wordWindow <= w.end && silent(w.start, w.start+wordWindow) {
			w.start += wordWindow
		}
		for w.end-wordWindow >= w.start && silent(w.end-wordWindow, w.end) {
			w.end -= wordWindow
		}
		if w.start+wordWindow > w.end {
			continue
		}
		hi := math.MaxInt
		if i > 0 {
			lo = out[i-1].end
		}
		if i+1 < len(out) {
			hi = out[i+1].start
		}
		for w.start-wordWindow >= lo && !silent(w.start-wordWindow, w.start) {
			w.start -= wordWindow
		}
		for w.end+wordWindow <= hi && !silent(w.end, w.end+wordWindow) {
			w.end += wordWindow
		}
		out[i] = w
	}
	return out
}

func wordRow(words []stamped, glyph func(from, to int) rune) row {
	// Anchors: each word's first and last letter at its start and end, with
	// time running linearly between them.
	var xs []float64
	var ts []int
	var text strings.Builder
	col := 0
	for i, w := range words {
		if i > 0 {
			text.WriteByte(' ')
			col++
		}
		r := []rune(w.text)
		lead, trail := 0, 0
		for lead < len(r) && unicode.IsPunct(r[lead]) {
			lead++
		}
		for trail < len(r)-lead && unicode.IsPunct(r[len(r)-1-trail]) {
			trail++
		}
		if lead+trail < len(r) {
			xs = append(xs, float64(col+lead), float64(col+len(r)-trail))
			ts = append(ts, w.start, w.end)
		}
		col += len(r)
		text.WriteString(w.text)
	}
	at := func(x float64) int {
		i := sort.SearchFloat64s(xs, x)
		switch {
		case len(xs) == 0:
			return 0
		case i == 0:
			return ts[0]
		case i == len(xs):
			return ts[len(ts)-1]
		}
		f := (x - xs[i-1]) / (xs[i] - xs[i-1])
		return ts[i-1] + int(f*float64(ts[i]-ts[i-1]))
	}
	levels := make([]rune, col)
	for x := range levels {
		from, to := at(float64(x)), at(float64(x+1))
		if to-from < wordWindow {
			mid := (from + to) / 2
			from, to = max(mid-wordWindow/2, 0), mid+wordWindow/2
		}
		levels[x] = glyph(from, to)
	}
	return row{string(levels), text.String(), len(words)}
}
