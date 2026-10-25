package tui

import (
	"time"

	"github.com/elhenro/bee/internal/zzz"
)

// internal tea.Msg types — kept here so model.go stays focused on Update
// dispatching.

type tickMsg time.Time

type iterMsg struct {
	n, max int
}

type phaseMsg string

type tokensMsg zzz.TokenStat

// commitsMsg marks the running iteration committed.
type commitsMsg struct{}

type logMsg struct {
	level string // "info" | "warn" | "err"
	text  string
}

type doneMsg struct {
	run *zzz.Run
	err error
}

// iterRow is one row in the timeline panel.
type iterRow struct {
	n      int
	status string // "running" | "committed" | "noop" | "repair" | "reset" | "failed"
	tokens zzz.TokenStat
}
