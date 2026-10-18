package loop

import (
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// syntheticPrefixes mark user-role messages the loop or compaction wrote, not
// the user.
var syntheticPrefixes = []string{"[compacted history]", "[nudge]", "[steer]", "[resume]", "[state card]", "[iter cap reached]"}

// isSyntheticUserText reports whether text is a loop-injected turn rather than
// a real user ask.
func isSyntheticUserText(text string) bool {
	t := strings.TrimSpace(text)
	for _, p := range syntheticPrefixes {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// cardGoalFor picks the state card goal for a Run: the user's text, unless it
// is a synthetic continuation, in which case the last real ask is kept so the
// original task stays on the wire. prior seeds the goal on a fresh process
// that resumed from a transcript.
func (e *Engine) cardGoalFor(userText string, prior []types.Message) string {
	if !isSyntheticUserText(userText) {
		e.cardGoal = userText
		return userText
	}
	if e.cardGoal == "" {
		e.cardGoal = firstUserText(prior)
	}
	if e.cardGoal != "" {
		return e.cardGoal
	}
	return userText
}
