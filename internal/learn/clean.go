package learn

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Clean makes model- or transcript-supplied text safe to print to a terminal:
// escape sequences are stripped and any remaining control rune becomes a space.
func Clean(s string) string {
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
