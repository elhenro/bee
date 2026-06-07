package tui

import (
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// hiveTaskWithContext builds the hive task from the current message, resolving
// bare continuations ("continue", "go on", …) against prior history. Without
// this, a plain "continue" reaches the planner verbatim: decompose finds no
// task, the fallback spawns one worker whose entire prompt is "continue", and
// that worker — a fresh engine with no conversation history — cancels with
// nothing to continue from. Here we re-anchor on the last real task and append
// the latest assistant progress so workers know what to do and where it stands.
func hiveTaskWithContext(content []types.ContentBlock, history []types.Message) string {
	cur := hiveTaskFromContent(content)
	if !isContinuation(cur) {
		return cur
	}
	anchor, progress := resolveTaskFromHistory(history)
	if anchor == "" {
		return cur // nothing to inherit; fall back to the literal message
	}
	var b strings.Builder
	b.WriteString(anchor)
	if progress != "" {
		b.WriteString("\n\n## Progress so far\n")
		b.WriteString(progress)
	}
	if cur != "" {
		fmt.Fprintf(&b, "\n\n## Now\nThe user said %q — continue the task above from where it stands.", cur)
	}
	return b.String()
}

// isContinuation reports whether s is a bare "keep going" message carrying no
// task of its own. Trailing punctuation is ignored.
func isContinuation(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimRight(s, ".!? ")
	switch s {
	case "", "continue", "go on", "go", "keep going", "keep going.",
		"proceed", "next", "more", "resume", "carry on", "yes", "y",
		"ok", "okay", "continue please", "go ahead":
		return true
	}
	return false
}

// resolveTaskFromHistory walks history newest-first for the anchor (most recent
// substantive user message) and progress (most recent assistant text). Progress
// is capped so a long transcript tail can't dominate the worker prompt.
func resolveTaskFromHistory(history []types.Message) (anchor, progress string) {
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]
		if msg.Ephemeral {
			continue
		}
		switch msg.Role {
		case types.RoleUser:
			if anchor != "" {
				continue
			}
			if t := flattenText(msg.Content); t != "" && !isContinuation(t) {
				anchor = t
			}
		case types.RoleAssistant:
			if progress == "" {
				if t := flattenText(msg.Content); t != "" {
					progress = truncateRunes(t, 1500)
				}
			}
		}
		if anchor != "" && progress != "" {
			break
		}
	}
	return anchor, progress
}

// flattenText joins the plain-text blocks of a message, dropping images.
func flattenText(blocks []types.ContentBlock) string {
	var texts []string
	for _, b := range blocks {
		if b.Type == types.BlockText {
			if t := strings.TrimSpace(b.Text); t != "" {
				texts = append(texts, t)
			}
		}
	}
	return strings.Join(texts, "\n\n")
}
