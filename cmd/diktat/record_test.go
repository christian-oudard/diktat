package main

import "testing"

func TestIsStop(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{"\x1b", true},
		{"\x03", true},
		{"ab\x03", true},
		{"\x1b[A", false},
		{"\x1bOP", false},
		{"q", false},
		{"", false},
	} {
		if got := isStop([]byte(c.in)); got != c.want {
			t.Errorf("isStop(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
