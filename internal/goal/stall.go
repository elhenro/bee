package goal

import "fmt"

// MaxIdenticalReplies is how many byte-identical replies in a row count as
// no progress. A changed reply resets the streak.
const MaxIdenticalReplies = 3

// ReplyStreak counts consecutive identical replies for one goal thread. It
// only reports stagnation; deciding whether a goal is met stays with the
// verdict, so a repeated reply alone never claims completion. The zero value
// is ready to use.
type ReplyStreak struct {
	tracker FingerprintTracker
	n       int
}

// Observe records a reply and reports whether the streak reached
// MaxIdenticalReplies. Empty replies carry no signal and leave it untouched.
func (r *ReplyStreak) Observe(reply string) bool {
	if NormalizeReply(reply) == "" {
		return false
	}
	if r.tracker.Observe("goal", reply) {
		r.n++
	} else {
		r.n = 1
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
