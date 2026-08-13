package human

import "testing"

// The precision rules are the whole point of this, so they are what is
// checked: whole MiB up to a GiB, one decimal past it, and none again once
// two digits carry the magnitude on their own.
func TestBytes(t *testing.T) {
	const MiB, GiB = 1 << 20, 1 << 30
	for _, c := range []struct {
		in   uint64
		want string
	}{
		// Sub-MiB precision is noise for anything measured here, and rounding
		// it away would say "0 MiB", which reads as nothing at all.
		{0, "<1 MiB"},
		{512 * 1024, "<1 MiB"},
		{MiB, "1 MiB"},
		{35 * MiB, "35 MiB"},
		{35*MiB + 700*1024, "36 MiB"},
		{1782 * MiB, "1.7 GiB"},
		// Rounding up to a whole unit changes the unit, rather than printing
		// a size in MiB that is really a GiB.
		{1023*MiB + 900*1024, "1.0 GiB"},
		{2*GiB + GiB/2, "2.5 GiB"},
		{12 * GiB, "12 GiB"},
		// Rounds to 10.0, which is where the decimal stops earning its place.
		{GiB*9 + GiB*97/100, "10 GiB"},
	} {
		if got := Bytes(c.in); got != c.want {
			t.Errorf("Bytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
