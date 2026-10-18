package tui

import "time"

// noteThinking starts the reasoning clock on the first delta after the
// buffer was empty. Called before the delta is appended.
func (m *Model) noteThinking() {
	if m.thinkPartial == "" {
		m.thinkStartedAt, m.thinkEndedAt = time.Now(), time.Time{}
	}
}

// answerStarted freezes the reasoning clock once the answer begins, so the
// live row reads "Thought for" instead of ticking through the answer.
func (m *Model) answerStarted() {
	if m.thinkPartial != "" && m.thinkEndedAt.IsZero() {
		m.thinkEndedAt = time.Now()
	}
}

// how long the in-flight reasoning has run, and whether it is still going
func (m Model) thinkElapsed() (d time.Duration, running bool) {
	if m.thinkStartedAt.IsZero() {
		return 0, true
	}
	if m.thinkEndedAt.IsZero() {
		return time.Since(m.thinkStartedAt), true
	}
	return m.thinkEndedAt.Sub(m.thinkStartedAt), false
}
