package models

import "testing"

func TestFeaturesRendering(t *testing.T) {
	for _, c := range []struct {
		name string
		want string
	}{
		{"parakeet-unified-en-0.6b", "streaming"},
		{"parakeet-tdt-0.6b-v3", ""},
	} {
		s, ok := Lookup(c.name)
		if !ok {
			t.Fatalf("%s is not on the menu", c.name)
		}
		if got := s.Features(); got != c.want {
			t.Errorf("%s: Features() = %q, want %q", c.name, got, c.want)
		}
	}
}

// The two parakeet-tdt entries are the offline models it is the streaming
// alternative to, so it is listed right after them.
func TestUnifiedFollowsParakeetTDT(t *testing.T) {
	names := Names()
	for i, name := range names {
		if name == "parakeet-unified-en-0.6b" {
			if i < 2 || names[i-2] != "parakeet-tdt-0.6b-v2" || names[i-1] != "parakeet-tdt-0.6b-v3" {
				t.Errorf("parakeet-unified-en-0.6b is at %d, after %v", i+1, names[:i])
			}
			return
		}
	}
	t.Error("parakeet-unified-en-0.6b is not on the menu")
}
