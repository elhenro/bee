package sandbox

import "testing"

func TestIsKnownSafe_RejectsExpansionHiddenFlags(t *testing.T) {
	for _, c := range []string{
		`find . -${Z:-exec} touch /tmp/x \;`,
		`find . {-exec,touch,/tmp/x,\;}`,
		`git diff ${Z:---output=/tmp/pwn}`,
		`tree ${Z:--o} /tmp/x`,
		`date ${Z:--s} 2020-01-01`,
		`rg ${Z:---pre=sh} x`,
	} {
		if IsKnownSafe(c) {
			t.Errorf("IsKnownSafe(%q) = true, want false", c)
		}
	}
	if !IsKnownSafe(`find . -type f`) || !IsKnownSafe(`git diff --stat`) {
		t.Error("plain read-only commands must stay safe")
	}
}
