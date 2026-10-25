package skills

import (
	"regexp"
	"strings"
	"unicode"
)

// scanShell inspects a recipe shell line that carries placeholders. inQuotes
// reports a {{ opener inside a single- or double-quoted region. Substitution
// single-quotes every value, so a placeholder the author also quoted would end
// up with the value's quotes cancelling theirs and the text running unquoted:
// `echo '{{x}}'` becomes `echo ”<x>”`. Rejecting it at parse time closes that
// door. unsafe is a coarse fail-closed verdict: shell syntax that lets a
// substituted value change meaning is rejected outright instead of parsed
// (heredocs, whose terminator a value can forge, and anything that could start
// a comment, which a newline in a value escapes). Parsing those correctly
// proved a losing game; when unsure, reject.
func scanShell(run string) (inQuotes, unsafe bool) {
	if !strings.Contains(run, "{{") {
		return false, false
	}
	// heredocs, and substitution contexts the scanner does not model: they
	// create quote-state differentials, so reject rather than extend it
	for _, bad := range []string{"<<", "$(", "`", "${"} {
		if strings.Contains(run, bad) {
			unsafe = true
		}
	}
	if !placeholdersStandalone(run) {
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

// placeholderRe finds every {{...}} opener-to-closer span.
var placeholderRe = regexp.MustCompile(`\{\{[^}]*\}\}`)

// placeholdersStandalone reports whether every placeholder is a whole shell
// word: preceded by start-of-run, whitespace or one of `;|&(`, and followed by
// whitespace, one of `;|&)` or end-of-run. A glued `\{{x}}` would let the
// backslash escape the opening quote the substitution adds, and `$`/word
// characters change how the quoted value splices into its neighbours.
func placeholdersStandalone(run string) bool {
	for _, m := range placeholderRe.FindAllStringIndex(run, -1) {
		if m[0] > 0 && !strings.ContainsRune(" \t\r\n;|&(", rune(run[m[0]-1])) {
			return false
		}
		if m[1] < len(run) && !strings.ContainsRune(" \t\r\n;|&)", rune(run[m[1]])) {
			return false
		}
	}
	return true
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
