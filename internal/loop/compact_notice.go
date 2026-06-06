package loop

import (
	"fmt"
	"time"

	"github.com/elhenro/bee/internal/types"
)

// emitCompactNotice appends an ephemeral scrollback card describing a completed
// auto-compaction to msgs and, when a live channel is wired, pushes it so the
// TUI renders it immediately instead of only at turn end. Ephemeral keeps it
// out of the next request (dropEphemeral strips it) — it's a UI notice, never
// model context, and it isn't persisted to the rollout. No-op when nothing
// actually shrank.
func (e *Engine) emitCompactNotice(msgs []types.Message, stats CompactStats) []types.Message {
	if stats.BeforeMsgs == stats.AfterMsgs {
		return msgs
	}
	note := types.Message{
		ID:        newID(),
		ParentID:  lastID(msgs),
		Role:      types.RoleAssistant,
		Content:   []types.ContentBlock{{Type: types.BlockText, Text: formatCompactNotice(stats)}},
		Time:      time.Now().UTC(),
		Ephemeral: true,
	}
	if e.LiveMsgCh != nil {
		select {
		case e.LiveMsgCh <- note:
		default:
		}
	}
	return append(msgs, note)
}

// dropEphemeral returns msgs without scrollback-only UI echoes. Auto-compact
// notices live in the message list for rendering but must never reach the
// model. Returns msgs unchanged (no copy) when none are ephemeral.
func dropEphemeral(msgs []types.Message) []types.Message {
	n := 0
	for _, m := range msgs {
		if m.Ephemeral {
			n++
		}
	}
	if n == 0 {
		return msgs
	}
	out := make([]types.Message, 0, len(msgs)-n)
	for _, m := range msgs {
		if m.Ephemeral {
			continue
		}
		out = append(out, m)
	}
	return out
}

// formatCompactNotice renders the post-auto-compact summary line.
func formatCompactNotice(s CompactStats) string {
	saved := s.BeforeTokens - s.AfterTokens
	return fmt.Sprintf("(auto-compacted · %s → %s tokens · −%s · %d→%d msgs · %s)",
		fmtTokens(s.BeforeTokens),
		fmtTokens(s.AfterTokens),
		fmtTokens(saved),
		s.BeforeMsgs,
		s.AfterMsgs,
		fmtDuration(s.Duration),
	)
}

// fmtTokens prints an int as "1.2k" / "12k" / "345".
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
