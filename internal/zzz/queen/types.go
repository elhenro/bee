package queen

import (
	"strings"
	"time"

	"github.com/elhenro/bee/internal/zzz"
)

// workerState is the supervisor's live view of one worker bee. Mutated only
// from Update (the bubbletea goroutine) so no lock is needed.
type workerState struct {
	idx     int
	id      string
	branch  string
	iter    int
	maxIt   int
	phase   string
	tokens  zzz.TokenStat
	commits int
	status  string // running | done | failed | aborted
	last    string // last log/subject line, trimmed
}

// per-worker tea.Msg variants, each tagged with the worker index so Update
// can route the mutation to the right row.
type (
	wIterMsg  struct{ idx, n, max int }
	wPhaseMsg struct {
		idx int
		p   string
	}
	wTokensMsg struct {
		idx int
		t   zzz.TokenStat
	}
	wCommitMsg struct{ idx int }
	wLogMsg    struct {
		idx   int
		text  string
		level string
	}
	wDoneMsg struct {
		idx int
		run *zzz.Run
		err error
	}
	tickMsg time.Time
)

// levelFor classifies a Println line for coloring — mirrors the single-run
// TUI's heuristic.
func levelFor(s string) string {
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "fail"), strings.Contains(low, "error"):
		return "err"
	case strings.Contains(low, "blocked"), strings.Contains(low, "denied"):
		return "warn"
	}
	return "info"
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
