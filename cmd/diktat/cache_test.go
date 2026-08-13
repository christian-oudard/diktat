package main

import (
	"slices"
	"testing"

	"github.com/christian-oudard/diktat/internal/config"
)

// The daemon holds every model it loads so switching back is instant. These
// cover the ceiling on that, which matters because ggml's context and compute
// buffers cost more than a small model's weights, and a laptop GPU shares its
// memory with the desktop.

func TestTouchOrdersByUse(t *testing.T) {
	d := &daemon{}
	d.touch("a")
	d.touch("b")
	d.touch("c")
	d.touch("a") // used again, so it moves to the back
	if want := []string{"b", "c", "a"}; !slices.Equal(d.lru, want) {
		t.Fatalf("lru = %v, want %v", d.lru, want)
	}
	// Touching must not duplicate: a duplicate would let the cache evict a
	// model that is still resident, and then close it twice.
	d.touch("a")
	if len(d.lru) != 3 {
		t.Errorf("touch duplicated an entry: %v", d.lru)
	}
}

func TestOverBudget(t *testing.T) {
	const mb = 1 << 20
	sizes := map[string]uint64{"old": 500 * mb, "mid": 500 * mb, "new": 500 * mb}
	lru := []string{"old", "mid", "new"} // "new" is the one in use

	for _, c := range []struct {
		name   string
		budget uint64
		want   []string
	}{
		{"everything fits", 2000 * mb, nil},
		{"drop the oldest", 1200 * mb, []string{"old"}},
		{"drop two", 600 * mb, []string{"old", "mid"}},
		// The model in use is never dropped, however small the budget.
		{"budget below one model", 1, []string{"old", "mid"}},
		{"zero budget", 0, []string{"old", "mid"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := overBudget(lru, sizes, c.budget); !slices.Equal(got, c.want) {
				t.Errorf("overBudget(%d MB) = %v, want %v", c.budget>>20, got, c.want)
			}
		})
	}
}

// TestOverBudgetSingleModel covers the degenerate case: one model, no budget.
// Freeing it would leave the daemon with nothing to transcribe with.
func TestOverBudgetSingleModel(t *testing.T) {
	got := overBudget([]string{"only"}, map[string]uint64{"only": 1 << 30}, 0)
	if got != nil {
		t.Errorf("overBudget dropped the model in use: %v", got)
	}
}

func TestCacheBudget(t *testing.T) {
	if got := cacheBudget(&config.Config{ModelCacheMB: 512}); got != 512<<20 {
		t.Errorf("configured budget = %d MB, want 512 MB", got>>20)
	}
	// Otherwise derived, and it has to hold at least one real model or the
	// daemon would reload on every switch.
	got := cacheBudget(&config.Config{})
	if got < 512<<20 {
		t.Errorf("derived budget %d MB is too small to hold a model", got>>20)
	}
	t.Logf("derived budget on this machine: %d MB", got>>20)
}
