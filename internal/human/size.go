// Package human renders quantities the way someone would say them, rather
// than the way they are stored.
package human

import (
	"fmt"
	"math"
)

const (
	MiB = 1 << 20
	GiB = 1 << 30
)

// Bytes renders a size in binary units, with the precision the magnitude
// deserves: a model file is not more legible for being 1782 MiB rather than
// 1.7 GiB, fractions of a MiB are noise at every size here, and past ten GiB
// two digits carry the magnitude without a decimal. Anything under a MiB is
// reported as such rather than rounded down to nothing.
func Bytes(n uint64) string {
	if n < MiB {
		return "<1 MiB"
	}
	if mib := (n + MiB/2) / MiB; mib < 1024 {
		return fmt.Sprintf("%d MiB", mib)
	}
	gib := float64(n) / GiB
	if math.Round(gib*10)/10 < 10 {
		return fmt.Sprintf("%.1f GiB", gib)
	}
	return fmt.Sprintf("%.0f GiB", gib)
}
