package goal

import (
	"fmt"
	"strings"
)

// MaxIdenticalReplies is how many byte-identical replies in a row count as
// no progress. A changed reply resets the streak.
const MaxIdenticalReplies = 3

// ReplyStreak counts consecutive identical replies for one goal thread. It
// only reports stagnation; deciding whether a goal is met stays with the
// verdict, so a repeated reply alone never claims completion. Replies compare
// with line endings normalized and outer whitespace trimmed; everything inside
// counts. The zero value is ready to use.
type ReplyStreak struct {
	last string // normalized previous non-empty reply
	n    int
}

// Observe records a reply and reports whether the streak reached
// MaxIdenticalReplies. Empty replies carry no signal and leave it untouched.
func (r *ReplyStreak) Observe(reply string) bool {
	norm := strings.ReplaceAll(strings.ReplaceAll(reply, "\r\n", "\n"), "\r", "\n")
	if norm = strings.TrimSpace(norm); norm == "" {
		return false
	}
	if norm == r.last {
		r.n++
	} else {
		r.last, r.n = norm, 1
	}
	return r.n >= MaxIdenticalReplies
}

// Count returns the current run of identical replies.
func (r *ReplyStreak) Count() int { return r.n }

// StalledReason is the typed stop reason for a no-progress streak of n
// identical replies.
func StalledReason(n int) string {
	return fmt.Sprintf("no progress, %d identical replies in a row", n)
}
