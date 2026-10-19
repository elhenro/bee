package llm

import (
	"regexp"
	"strings"
)

// completeCallsEnd walks s left to right over known `<name>` calls and returns
// the end offset of the last call whose own close tag is present, plus whether
// any known open tag was seen. A close tag that merely sits inside another
// call's JSON string never counts: the outermost call must close for itself, or
// the walk stops before it and leaves it buffered.
func completeCallsEnd(s string, known map[string]bool) (end int, sawOpen bool) {
	cur := 0
	for cur < len(s) {
		loc := openTagRe.FindStringSubmatchIndex(s[cur:])
		if loc == nil {
			break
		}
		name := strings.ToLower(s[cur+loc[2] : cur+loc[3]])
		bodyStart := cur + loc[1]
		cur = bodyStart
		if !known[name] {
			continue
		}
		sawOpen = true
		scanFrom := bodyStart
		i := bodyStart
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n') {
			i++
		}
		if i < len(s) && s[i] == '{' {
			m := matchBraces(s, i)
			if m < 0 {
				return end, sawOpen
			}
			scanFrom = m + 1
		}
		cl := regexp.MustCompile(`(?i)</` + regexp.QuoteMeta(name) + `>`).FindStringIndex(s[scanFrom:])
		if cl == nil {
			return end, sawOpen
		}
		end = scanFrom + cl[1]
		cur = end
	}
	return end, sawOpen
}

// openCallPending reports whether s holds a known `<name>` call that has not
// closed yet, i.e. text appended to s lands inside that call's body.
func openCallPending(s string, known map[string]bool) bool {
	end, sawOpen := completeCallsEnd(s, known)
	if !sawOpen {
		return false
	}
	for _, loc := range openTagRe.FindAllStringSubmatchIndex(s[end:], -1) {
		if known[strings.ToLower(s[end+loc[2]:end+loc[3]])] {
			return true
		}
	}
	return false
}
