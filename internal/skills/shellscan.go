package skills

import (
	"regexp"
	"strings"
)

// heredocRe matches the operator and delimiter word after `<<` / `<<-`.
// groups: 1 dash, 2 single-quoted, 3 double-quoted, 4 backslashed or bare.
var heredocRe = regexp.MustCompile(`^<<(-?)[ \t]*(?:'([^'\n]+)'|"([^"\n]+)"|\\?([A-Za-z_][A-Za-z0-9_]*))`)

type heredoc struct {
	delim  string
	quoted bool // body is literal, the shell expands nothing in it
	strip  bool // <<- allows tab-indented terminator
}

// scanShell walks a recipe shell line tracking quote state and heredoc
// bodies. It reports a {{ opener inside quotes and one inside the body of an
// unquoted heredoc (where the shell still expands $(...) and backticks, so a
// substituted value is not inert).
func scanShell(run string) (inQuotes, inOpenHeredoc bool) {
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
		case c == '<' && !inSingle && !inDouble && strings.HasPrefix(run[i:], "<<") &&
			(i == 0 || run[i-1] != '<') && !strings.HasPrefix(run[i:], "<<<"):
			if m := heredocRe.FindStringSubmatch(run[i:]); m != nil {
				h := heredoc{strip: m[1] == "-"}
				switch {
				case m[2] != "":
					h.delim, h.quoted = m[2], true
				case m[3] != "":
					h.delim, h.quoted = m[3], true
				default:
					h.delim = m[4]
					h.quoted = strings.Contains(m[0], "\\") // <<\EOF is literal too
				}
				pending = append(pending, h)
				i += len(m[0]) - 1
			}
		case c == '\n' && !inSingle && !inDouble && len(pending) > 0:
			var body bool
			i, body = skipHeredocs(run, i+1, pending)
			inOpenHeredoc = inOpenHeredoc || body
			pending = nil
		}
	}
	return inQuotes, inOpenHeredoc
}

// skipHeredocs consumes the bodies of pending heredocs starting at run[start]
// and returns the index of the last consumed byte plus whether an unquoted
// body held a placeholder opener.
func skipHeredocs(run string, start int, pending []heredoc) (int, bool) {
	pos, found := start, false
	for _, h := range pending {
		for pos < len(run) {
			end := strings.IndexByte(run[pos:], '\n')
			line := run[pos:]
			if end >= 0 {
				line = run[pos : pos+end]
			}
			next := len(run)
			if end >= 0 {
				next = pos + end + 1
			}
			if h.strip {
				line = strings.TrimLeft(line, "\t")
			}
			pos = next
			if line == h.delim {
				break
			}
			if !h.quoted && strings.Contains(line, "{{") {
				found = true
			}
		}
	}
	return pos - 1, found
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

// placeholderInHeredoc reports a {{ opener in an unquoted heredoc body.
func placeholderInHeredoc(run string) bool {
	_, h := scanShell(run)
	return h
}
