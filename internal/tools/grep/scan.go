package grep

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"unicode/utf8"
)

// maxLineWidth caps an emitted line so one minified-bundle hit cannot eat the
// whole result budget.
const maxLineWidth = 300

// eachLine calls fn with every line of f and its 1-based number until fn
// returns false. A scan error just ends the file.
func eachLine(f *os.File, fn func(n int, line []byte) bool) {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for n := 1; sc.Scan(); n++ {
		if !fn(n, sc.Bytes()) {
			return
		}
	}
}

// clipLine renders line, cutting it to maxLineWidth runes and noting how many
// bytes were dropped.
func clipLine(line []byte) string {
	if len(line) <= maxLineWidth {
		return string(line)
	}
	i := 0
	for n := 0; n < maxLineWidth && i < len(line); n++ {
		_, size := utf8.DecodeRune(line[i:])
		i += size
	}
	if i >= len(line) {
		return string(line)
	}
	return fmt.Sprintf("%s… [+%d bytes]", line[:i], len(line)-i)
}

type numbered struct {
	n    int
	text string
}

// scanWithContext streams f and emits up to budget result strings with ctxN
// surrounding lines per match. Match lines use ':' separator; context lines
// use '-'. Adjacent windows merge naturally because each line is emitted at
// most once: only unemitted lines wait in the before buffer, so it is always
// the contiguous run just ahead of the current line.
func scanWithContext(f *os.File, re *regexp.Regexp, path string, ctxN, budget int) []string {
	if budget <= 0 {
		return nil
	}
	var out []string
	var before []numbered
	after := 0
	emit := func(n int, sep, text string) bool {
		out = append(out, fmt.Sprintf("%s:%d%s%s", path, n, sep, text))
		return len(out) < budget
	}
	eachLine(f, func(n int, line []byte) bool {
		switch {
		case re.Match(line):
			for _, b := range before {
				if !emit(b.n, "-", b.text) {
					return false
				}
			}
			before = before[:0]
			after = ctxN
			return emit(n, ":", clipLine(line))
		case after > 0:
			after--
			return emit(n, "-", clipLine(line))
		default:
			before = append(before, numbered{n, clipLine(line)})
			if len(before) > ctxN {
				before = before[1:]
			}
			return true
		}
	})
	return out
}
