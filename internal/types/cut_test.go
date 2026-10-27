package types

import (
	"testing"
	"unicode/utf8"
)

func TestCutBytes(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 3, "hel"},
		{"hello", 0, ""},
		{"hello", -1, ""},
		{"aé", 2, "a"},
		{"aé", 3, "aé"},
		{"世界", 4, "世"},
		{"", 3, ""},
	}
	for _, c := range cases {
		got := CutBytes(c.in, c.n)
		if got != c.want || !utf8.ValidString(got) {
			t.Errorf("CutBytes(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
