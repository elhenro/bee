package types

import "unicode/utf8"

// CutBytes returns the longest prefix of s that fits in n bytes without
// splitting a UTF-8 sequence.
func CutBytes(s string, n int) string {
	if n >= len(s) {
		return s
	}
	if n < 0 {
		n = 0
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
