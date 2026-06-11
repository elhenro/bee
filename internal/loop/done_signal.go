package loop

import "strings"

// doneSignalToken is the exact sentinel the model may emit to declare an
// open-ended task complete. Matched case-insensitively as an inline tag.
const doneSignalToken = "<promise>done</promise>"

// detectDoneSignal reports whether the assistant message contains a "DONE"
// sentinel the model uses to declare it finished an open-ended task.
// Case-insensitive. Hardened against false positives: the token must sit
// OUTSIDE any fenced code block (so a file read or diff echoing the tag does
// not trip an exit) AND appear on the final non-empty line (the model's
// concluding statement), not buried mid-paragraph.
func detectDoneSignal(s string) bool {
	if s == "" {
		return false
	}
	cleaned := stripFencedCode(s)
	last := strings.ToLower(lastNonEmptyLine(cleaned))
	idx := strings.LastIndex(last, doneSignalToken)
	if idx < 0 {
		return false
	}
	// the sentinel must close the line — only trailing whitespace/punctuation
	// may follow. prose after it ("...done means finished, but...") is the model
	// talking about the tag, not emitting it as a terminal signal.
	rest := strings.TrimRight(last[idx+len(doneSignalToken):], " \t.!?)\"'`*")
	return rest == ""
}

// stripFencedCode removes ``` fenced blocks so a done token quoted inside code
// (a read of a file that mentions it, a pasted diff) is not treated as a real
// completion signal. Unterminated fences drop everything from the fence on.
func stripFencedCode(s string) string {
	var b strings.Builder
	inFence := false
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if !inFence {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// lastNonEmptyLine returns the final line with non-whitespace content.
func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}
