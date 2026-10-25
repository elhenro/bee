package wire

import "iter"

// OutsideStrings yields index and byte of every byte of s outside a JSON
// string literal. quotes delimiting a literal are yielded, its contents
// (escapes included) skipped, so structural scans see only braces, brackets,
// commas and quotes.
func OutsideStrings[T ~string | ~[]byte](s T) iter.Seq2[int, byte] {
	return func(yield func(int, byte) bool) {
		inStr, esc := false, false
		for i := 0; i < len(s); i++ {
			c := s[i]
			switch {
			case esc:
				esc = false
			case inStr && c == '\\':
				esc = true
			case inStr && c != '"':
			default:
				inStr = c == '"' && !inStr
				if !yield(i, c) {
					return
				}
			}
		}
	}
}
