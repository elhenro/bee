package tui

import "testing"

func TestPrettyCwd_MatchesWholePathElements(t *testing.T) {
	old := osUserHome
	osUserHome = func() (string, error) { return "/Users/henry", nil }
	defer func() { osUserHome = old }()
	cases := map[string]string{
		"/Users/henry":          "~",
		"/Users/henry/projects": "~/projects",
		"/Users/henryschober/x": "/Users/henryschober/x",
		"/tmp/work":             "/tmp/work",
	}
	for in, want := range cases {
		if got := prettyCwd(in); got != want {
			t.Errorf("prettyCwd(%q) = %q, want %q", in, got, want)
		}
	}
}
