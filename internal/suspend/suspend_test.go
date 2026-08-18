package suspend

import (
	"testing"
	"time"
)

func TestTotal(t *testing.T) {
	a := Total()
	if a < 0 {
		t.Fatalf("suspend total is negative: %s", a)
	}
	// The machine did not sleep between two adjacent calls, so the readings
	// may differ only by the jitter of reading two clocks a syscall apart.
	if b := Total(); b-a > time.Second || a-b > time.Second {
		t.Errorf("suspend total moved without a suspend: %s then %s", a, b)
	}
}
