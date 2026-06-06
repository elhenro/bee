package tui

import "testing"

func TestSplitPathLine(t *testing.T) {
	cases := []struct {
		in       string
		wantFile string
		wantLine int
	}{
		{"main.go:12", "main.go", 12},
		{"main.go", "main.go", 0},
		{"", "", 0},
		{"pkg/app.go:3", "pkg/app.go", 3},
		{"main.go:abc", "main.go:abc", 0}, // non-numeric suffix: treat whole as path
		{":12", ":12", 0},                 // no path before colon
	}
	for _, c := range cases {
		f, l := splitPathLine(c.in)
		if f != c.wantFile || l != c.wantLine {
			t.Errorf("splitPathLine(%q)=(%q,%d) want (%q,%d)", c.in, f, l, c.wantFile, c.wantLine)
		}
	}
}
