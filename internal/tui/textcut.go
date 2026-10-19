package tui

import "unicode/utf8"

// cutBytes returns s cut to at most n bytes, backing off to a rune boundary
// so a multibyte rune is never split.
func cutBytes(s string, n int) string {
	if n >= len(s) {
		return s
	}
	if n <= 0 {
		return ""
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
