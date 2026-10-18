package skills

import (
	"regexp"
	"strings"
)

// heredocRe matches the operator and delimiter word after `<<` / `<<-`.
// groups: 1 dash, 2 single-quoted, 3 double-quoted, 4 backslashed or bare.
var heredocRe = regexp.MustCompile(`^<<(-?)[ \t]*(?:'([^'\n]+)'|"([^"\n]+)"|\\?([A-Za-z_][A-Za-z0-9_]*))`)

type heredoc struct {
	delim string
	strip bool // <<- allows tab-indented terminator
}

// scanShell walks a recipe shell line tracking quote state, comments and
// heredocs. inQuotes reports a {{ opener inside quotes. unsafe is fail-closed:
// it reports a placeholder the executor cannot substitute safely, i.e. in a
// comment, in any heredoc body (even a quoted one: a value holding a line
// equal to the terminator ends the heredoc early and the rest runs as shell),
// or anywhere in a run whose heredoc syntax cannot be parsed cleanly.
func scanShell(run string) (inQuotes, unsafe bool) {
	hasPlaceholder := strings.Contains(run, "{{")
	if !hasPlaceholder {
		return false, false
	}
	inSingle, inDouble := false, false
	var pending []heredoc
	for i := 0; i < len(run); i++ {
		c := run[i]
		switch {
		case c == '\\' && !inSingle:
			i++ // escaped char never changes quote state
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '{' && i+1 < len(run) && run[i+1] == '{' && (inSingle || inDouble):
			inQuotes = true
		case c == '#' && !inSingle && !inDouble && wordStart(run, i):
			end := strings.IndexByte(run[i:], '\n')
			if end < 0 {
				end = len(run) - i
			}
			// a substituted value with a newline would escape the comment
			if strings.Contains(run[i:i+end], "{{") {
				unsafe = true
			}
			i += end - 1
		case c == '<' && !inSingle && !inDouble && strings.HasPrefix(run[i:], "<<"):
			if strings.HasPrefix(run[i:], "<<<") {
				i += 2 // here-string, not a heredoc
				continue
			}
			h, n, ok := parseHeredoc(run[i:])
			if !ok {
				return inQuotes, true
			}
			pending = append(pending, h)
			i += n - 1
		case c == '\n' && !inSingle && !inDouble && len(pending) > 0:
			var bad bool
			i, bad = skipHeredocs(run, i+1, pending)
			unsafe = unsafe || bad
			pending = nil
		}
	}
	if len(pending) > 0 { // operator on the last line, no body at all
		unsafe = true
	}
	return inQuotes, unsafe
}

// wordStart reports whether run[i] begins a shell word (a # there is a comment).
func wordStart(run string, i int) bool {
	if i == 0 {
		return true
	}
	return strings.ContainsRune(" \t\n;|&(", rune(run[i-1]))
}

// parseHeredoc reads `<<[-]WORD` at the start of s. ok is false when the
// delimiter is anything but a plain word, optionally wholly quoted, followed
// by a word boundary.
func parseHeredoc(s string) (h heredoc, n int, ok bool) {
	m := heredocRe.FindStringSubmatch(s)
	if m == nil {
		return h, 0, false
	}
	n = len(m[0])
	if n < len(s) && !strings.ContainsRune(" \t\n;|&()<>", rune(s[n])) {
		return h, 0, false // embedded quotes or metachar in the word
	}
	h.strip = m[1] == "-"
	switch {
	case m[2] != "":
		h.delim = m[2]
	case m[3] != "":
		h.delim = m[3]
	default:
		h.delim = m[4]
	}
	return h, n, true
}

// skipHeredocs consumes the bodies of pending heredocs starting at run[start]
// and returns the index of the last consumed byte plus whether any body held a
// placeholder opener or a terminator was never found.
func skipHeredocs(run string, start int, pending []heredoc) (int, bool) {
	pos, bad := start, false
	for _, h := range pending {
		closed := false
		for pos < len(run) && !closed {
			line := run[pos:]
			next := len(run)
			if end := strings.IndexByte(line, '\n'); end >= 0 {
				line = line[:end]
				next = pos + end + 1
			}
			pos = next
			if h.strip {
				line = strings.TrimLeft(line, "\t")
			}
			if line == h.delim {
				closed = true
			} else if strings.Contains(line, "{{") {
				bad = true
			}
		}
		if !closed {
			bad = true
		}
	}
	return pos - 1, bad
}

// placeholderInQuotes reports whether a {{...}} opener sits inside a single- or
// double-quoted region of a shell line. Substitution single-quotes every value,
// so a placeholder the author also quoted would end up with the value's quotes
// cancelling theirs and the text running unquoted: `echo '{{x}}'` becomes
// `echo ”<x>”`. Rejecting it at parse time closes that door.
func placeholderInQuotes(run string) bool {
	q, _ := scanShell(run)
	return q
}

// placeholderInHeredoc reports a placeholder that heredoc or comment syntax
// makes unsafe to substitute (see scanShell).
func placeholderInHeredoc(run string) bool {
	_, u := scanShell(run)
	return u
}
