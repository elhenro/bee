package loop

import "github.com/elhenro/bee/internal/types"

// cardGoalFor picks the state card goal for a Run: the user's text, unless it
// is a synthetic continuation, in which case the last real ask is kept so the
// original task stays on the wire. prior seeds the goal on a fresh process
// that resumed from a transcript.
func (e *Engine) cardGoalFor(userText string, prior []types.Message) string {
	if !types.IsSyntheticUserText(userText) {
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
