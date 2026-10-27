package wire

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncForErrKeepsRunesWhole(t *testing.T) {
	for _, s := range []string{strings.Repeat("€", 100), "a" + strings.Repeat("€", 100), "ab" + strings.Repeat("€", 100)} {
		if got := truncForErr([]byte(s)); !utf8.ValidString(got) {
			t.Errorf("cut mid-rune: %q", got)
		}
	}
}
