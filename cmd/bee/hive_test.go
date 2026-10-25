package main

import (
	"testing"
	"unicode/utf8"
)

func TestEllipsize(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"abcdefghijkl", 10, "abcdefg..."},
		// multi-byte runes must never be cut mid-sequence
		{"äöüäöüäöüäöü", 8, "äöüäö..."},
	}
	for _, c := range cases {
		got := ellipsize(c.in, c.n)
		if got != c.want {
			t.Errorf("ellipsize(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("ellipsize(%q, %d) = %q is not valid utf-8", c.in, c.n, got)
		}
	}
}
