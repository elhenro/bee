package tui

import (
	"fmt"
	"time"

	"github.com/elhenro/bee/internal/loop"
)

// formatCompactDone renders the post-/compact summary line. Falls back to a
// bare "done" tag for the no-op short-history path where stats are zeroed.
func formatCompactDone(s loop.CompactStats) string {
	if s.BeforeMsgs == 0 && s.AfterMsgs == 0 {
		return "(/compact done)"
	}
	if s.BeforeMsgs == s.AfterMsgs {
		return fmt.Sprintf("(/compact done · history short, no change · %s)", fmtDuration(s.Duration))
	}
	saved := s.BeforeTokens - s.AfterTokens
	return fmt.Sprintf("(/compact done · %s → %s tokens · −%s · %d→%d msgs · %s)",
		fmtTokens(s.BeforeTokens),
		fmtTokens(s.AfterTokens),
		fmtTokens(saved),
		s.BeforeMsgs,
		s.AfterMsgs,
		fmtDuration(s.Duration),
	)
}

// fmtTokens prints an int as "1.2k" / "12k" / "345". Keeps the line compact.
func fmtTokens(n int) string {
	if n < 0 {
		return "-" + fmtTokens(-n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 10_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%dk", n/1000)
}

// fmtDuration shows sub-second as "850ms", otherwise "1.2s" / "12s".
func fmtDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < 10*time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// bytesHuman renders a byte count as B/KiB/MiB. Used for staged-image hints.
func bytesHuman(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%d KiB", n/1024)
	}
	return fmt.Sprintf("%d MiB", n/(1024*1024))
}
