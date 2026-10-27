package llm

import "strings"

// closeTagsOf lists the closing tag of every known tool.
func closeTagsOf(known map[string]bool) []string {
	tags := make([]string, 0, len(known))
	for name := range known {
		tags = append(tags, "</"+name+">")
	}
	return tags
}

// hasCloseTagSince reports whether a closing tag in tags ends inside s[from:].
// Cheaper than firing the regex extractor on every token-sized delta, and it
// scans only the new bytes plus the overlap a tag split across deltas needs,
// so a whole stream costs O(total) instead of O(total^2).
func hasCloseTagSince(s string, from int, tags []string) bool {
	for _, tag := range tags {
		if strings.Contains(s[max(from-(len(tag)-1), 0):], tag) {
			return true
		}
	}
	return false
}

// extractIncremental runs the hermes normalizer + tool extractors on the
// current buffer and returns (early-dispatched calls, remaining buf). The
// remaining buffer is what comes AFTER the last successful close tag — text
// past that point hasn't been parsed yet and stays for the next delta cycle.
//
// Conservative: when no closed tag is found, returns (nil, buf unchanged)
// so the caller knows nothing was consumed.
func extractIncremental(s string, known map[string]bool, canonical map[string]string) ([]parsedCall, string) {
	// find the last close tag position. anything past it is partial and
	// must stay in the buffer.
	lastClose := -1
	for name := range known {
		tag := "</" + name + ">"
		if idx := strings.LastIndex(s, tag); idx >= 0 {
			end := idx + len(tag)
			if end > lastClose {
				lastClose = end
			}
		}
	}
	// close tags inside another call's JSON string must not trigger dispatch:
	// when a known call is open, only fully closed calls count
	if end, sawOpen := completeCallsEnd(s, known); sawOpen {
		lastClose = end
	}
	if lastClose <= 0 {
		return nil, s
	}
	head := s[:lastClose]
	tail := s[lastClose:]
	normalized := normalizeHermesEnvelopes(head, known)
	calls, cleaned := extractToolCalls(normalized, known, canonical)
	if len(calls) == 0 {
		return nil, s
	}
	// preserve surrounding prose: cleaned is `head` with tool tags removed,
	// tail is the not-yet-parsed text past the last close. EventDone's final
	// extractToolCalls pass will see `cleaned + tail` — no closed tags
	// remain in cleaned (extractor already consumed them), so it falls
	// through as plain text in the final EventTextDelta.
	remaining := cleaned
	if tail != "" {
		if remaining != "" {
			remaining += " "
		}
		remaining += tail
	}
	return calls, remaining
}
