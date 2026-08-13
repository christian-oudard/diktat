package models

import (
	"strconv"
	"testing"
)

// Selecting by menu number is the short way in, since the names run to
// twenty-odd characters.
func TestLookupByNumber(t *testing.T) {
	first, last := Catalog[0], Catalog[len(Catalog)-1]
	for _, c := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"1", first.Name, true},
		{strconv.Itoa(len(Catalog)), last.Name, true},
		{first.Name, first.Name, true},
		// Out of range, and the off-by-one that a 0-based reading would hit.
		{"0", "", false},
		{strconv.Itoa(len(Catalog) + 1), "", false},
		{"-1", "", false},
		{"", "", false},
		{"nope", "", false},
		// A path is not a menu entry, and must not be read as one.
		{"./3", "", false},
	} {
		got, ok := Lookup(c.in)
		if ok != c.ok || got.Name != c.want {
			t.Errorf("Lookup(%q) = %q,%v; want %q,%v", c.in, got.Name, ok, c.want, c.ok)
		}
	}
}

// Resolve has to agree with Lookup, or `diktat model 3` would check one
// model and switch to another.
func TestResolveByNumber(t *testing.T) {
	spec, _ := Lookup("2")
	if got := Resolve("2"); got != spec.Path() {
		t.Errorf("Resolve(\"2\") = %q, want %q", got, spec.Path())
	}
}

// A name that is also a number would be ambiguous. Nothing in the menu is,
// and names win if one ever is, but the catalog should not grow one by
// accident.
func TestNoNumericModelNames(t *testing.T) {
	for _, s := range Catalog {
		if _, err := strconv.Atoi(s.Name); err == nil {
			t.Errorf("%q is a number, which collides with menu-number selection", s.Name)
		}
	}
}
