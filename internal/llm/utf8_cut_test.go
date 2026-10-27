package llm

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// every cut point lands inside a multibyte rune for some n; none may emit
// invalid UTF-8.
func TestTruncateHelpersKeepRunesWhole(t *testing.T) {
	s := strings.Repeat("a€", 300)
	for n := 1; n < 8; n++ {
		if got := truncate(s, 200+n); !utf8.ValidString(got) {
			t.Fatalf("truncate cut mid-rune at %d: %q", 200+n, got[len(got)-8:])
		}
	}
	if got := truncateForNudge(strings.Repeat("€", 200)); !utf8.ValidString(got) {
		t.Errorf("truncateForNudge cut mid-rune")
	}
}
