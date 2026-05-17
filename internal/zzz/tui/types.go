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

type commitsMsg int

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
	n       int
	status  string // "running" | "committed" | "noop" | "reset" | "failed"
	subject string
	tokens  zzz.TokenStat
	commit  string // short sha
	when    time.Time
}
