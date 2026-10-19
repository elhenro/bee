package safety

import "strings"

// hasHiddenExpansion reports expansions the shell performs after the lexical
// check, so the argv judged here is not the argv that runs: parameter
// expansion ($VAR, ${..}, $[..]), backticks and brace expansion ({a,b},
// {1..3}). Fails closed: an escaped $ or { is flagged too. Command and
// process substitution are covered by hasSubstitution.
func hasHiddenExpansion(t Token) bool {
	var unquoted strings.Builder
	for _, s := range t.Segments {
		if s.Quote == QuoteSingle {
			unquoted.WriteByte('x')
			continue
		}
		if strings.Contains(s.Text, "`") || hasParamExpansion(s.Text) {
			return true
		}
		if s.Quote == QuoteNone {
			unquoted.WriteString(s.Text)
		} else {
			unquoted.WriteByte('x') // braces never expand inside double quotes
		}
	}
	return hasBraceExpansion(unquoted.String())
}

// hasParamExpansion reports a $ that starts an expansion; a trailing or
// space-followed $ (a regex anchor in "foo$") is literal.
func hasParamExpansion(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '$' {
			continue
		}
		c := s[i+1]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			return true
		case strings.IndexByte("_{([@*#?!$-", c) >= 0:
			return true
		}
	}
	return false
}

// hasBraceExpansion reports an unquoted {..} holding a comma or a range.
func hasBraceExpansion(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		end := strings.IndexByte(s[i:], '}')
		if end < 0 {
			continue
		}
		body := s[i+1 : i+end]
		if strings.Contains(body, ",") || strings.Contains(body, "..") {
			return true
		}
	}
	return false
}
