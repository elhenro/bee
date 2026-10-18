package skills

import (
	"strings"
	"unicode"
)

// scanShell inspects a recipe shell line that carries placeholders. inQuotes
// reports a {{ opener inside a quoted region. unsafe is a coarse fail-closed
// verdict: shell syntax that lets a substituted value change meaning is
// rejected outright instead of parsed (heredocs, whose terminator a value can
// forge, and anything that could start a comment, which a newline in a value
// escapes). Parsing those correctly proved a losing game; when unsure, reject.
func scanShell(run string) (inQuotes, unsafe bool) {
	if !strings.Contains(run, "{{") {
		return false, false
	}
	if strings.Contains(run, "<<") {
		unsafe = true
	}
	inSingle, inDouble := false, false
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
		case c == '#' && !inSingle && !inDouble && couldStartComment(run, i):
			unsafe = true
		}
	}
	if inSingle || inDouble {
		unsafe = true // unbalanced quotes: unsure, reject
	}
	return inQuotes, unsafe
}

// couldStartComment is true unless the # is clearly mid-word: after an
// alphanumeric, `$` ($#) or `{` (${#x}).
func couldStartComment(run string, i int) bool {
	if i == 0 {
		return true
	}
	p := rune(run[i-1])
	return !(p == '$' || p == '{' || (p < unicode.MaxASCII && (unicode.IsLetter(p) || unicode.IsDigit(p))))
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

// placeholderInUnsafeSyntax reports a templated run that uses a heredoc or a
// possible comment (see scanShell).
func placeholderInUnsafeSyntax(run string) bool {
	_, u := scanShell(run)
	return u
}
