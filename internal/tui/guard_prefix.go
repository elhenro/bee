package tui

import "strings"

// guardTags are the bracket markers the loop prepends to tool-result content
// to steer the model (repeat / stall / budget / verify nudges). They live in
// the same Content string the model reads, so they also reach the TUI. The
// human view strips them unless show_nudges is on — they're guidance for the
// model, noise for the reader. Source of these markers:
//   - internal/loop/turn_repeat.go      [repeat] [two-strike] [tool-fail]
//   - internal/loop/turn_warnings.go    [stall] [iter] [tokens]
//   - internal/loop/turn_idempotency.go [dup-write]
//   - internal/loop/edit_verify.go      [verify]
//   - internal/prompt/context_warning.go [context …]
//
// keyed on the first word inside the brackets so arg-carrying tags
// ("iter 3/6", "context at 80%", "stall 12 iters") still match.
var guardTags = map[string]bool{
	"repeat":     true,
	"two-strike": true,
	"tool-fail":  true,
	"stall":      true,
	"iter":       true,
	"tokens":     true,
	"context":    true,
	"dup-write":  true,
	"verify":     true,
	"nudge":      true,
}

// stripGuardPrefixes removes leading guard-warning paragraphs (each a
// `[tag …]\n\n` block) from a tool-result body. Stops at the first paragraph
// that isn't a known guard tag, so real output that happens to start with `[`
// (log lines, JSON arrays) is preserved. Warnings stack, so it loops.
func stripGuardPrefixes(s string) string {
	for strings.HasPrefix(s, "[") {
		end := strings.IndexByte(s, ']')
		if end < 0 {
			return s
		}
		tag := s[1:end]
		if sp := strings.IndexByte(tag, ' '); sp >= 0 {
			tag = tag[:sp]
		}
		if !guardTags[tag] {
			return s
		}
		// drop this paragraph through its blank-line separator; if there's no
		// separator the warning was the whole body.
		nl := strings.Index(s, "\n\n")
		if nl < 0 {
			return ""
		}
		s = s[nl+2:]
	}
	return s
}
