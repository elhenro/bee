package loop

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/elhenro/bee/internal/types"
)

// specStartRatio is the fraction of the hard auto-compact trigger at which a
// background summarization starts. The result is held until the hard trigger
// fires, so the configured threshold keeps its meaning; what changes is that the
// swap lands without a model round trip stalling the loop.
const specStartRatio = 0.85

// specCompact is one in-flight (or finished) background summarization of a
// message prefix.
type specCompact struct {
	cancel  context.CancelFunc
	done    chan struct{}
	snapLen int    // len(msgs) when the snapshot was taken
	lastID  string // id of the snapshot's last message, to detect a replaced history
	out     []types.Message
	stats   CompactStats
	err     error
}

// matches reports whether msgs still extends the snapshot. A sync compaction
// (budget recovery) replaces the history and invalidates it.
func (s *specCompact) matches(msgs []types.Message) bool {
	return s.snapLen > 0 && s.snapLen <= len(msgs) && msgs[s.snapLen-1].ID == s.lastID
}

// autoCompactStep is the loop-top compaction check. Under the hard trigger it
// swaps in a finished background summary (or waits for the one in flight); only
// without one does it summarize inline. Between specStartRatio and the trigger
// it starts that background summary.
func (e *Engine) autoCompactStep(ctx context.Context, msgs []types.Message, sys string) []types.Message {
	if !e.compactionEnabled() {
		return msgs
	}
	budget := contextBudget(e.Cfg)
	threshold := scaledCompactThreshold(e.Cfg.Compaction.Threshold, budget)
	pressure := compactPressure(sys, msgs, e.run.lastInputTokens, budget, threshold)
	if pressure <= specStartRatio || !compactWorthwhile(msgs, budget) {
		return msgs
	}
	if pressure <= 1 {
		e.startSpecCompact(ctx, msgs)
		return msgs
	}
	if out, ok := e.takeSpecCompact(ctx, msgs); ok {
		e.run.lastInputTokens = 0
		return out
	}
	return e.compactInline(ctx, msgs)
}

// compactInline summarizes msgs on the spot and returns the shorter history
// with its notice. A failure keeps msgs.
func (e *Engine) compactInline(ctx context.Context, msgs []types.Message) []types.Message {
	compacted, stats, err := e.compact(ctx, msgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "loop: auto-compact failed: %v\n", err)
		return msgs
	}
	// post-compact: reset lastInputTokens so the next iteration re-evaluates
	// against the smaller history.
	e.run.lastInputTokens = 0
	return e.emitCompactNotice(compacted, stats)
}

// startSpecCompact summarizes the current history in the background. No-op while
// one is in flight or after one failed this Run (no retry storm).
func (e *Engine) startSpecCompact(ctx context.Context, msgs []types.Message) {
	if e.run.spec != nil || e.run.specFailed || len(msgs) == 0 {
		return
	}
	// child of the Run ctx: a canceled Run stops the summary, and
	// cancelSpecCompact ends it when the Run returns with the result unused.
	ctx, cancel := context.WithCancel(ctx)
	s := &specCompact{
		cancel:  cancel,
		done:    make(chan struct{}),
		snapLen: len(msgs),
		lastID:  msgs[len(msgs)-1].ID,
	}
	snap := dropEphemeral(append([]types.Message(nil), msgs...))
	prov, model := e.Provider, compactionModel(e.Cfg)
	e.run.spec = s
	go func() {
		defer close(s.done)
		s.out, s.stats, s.err = Compact(ctx, prov, model, snap)
	}()
}

// takeSpecCompact swaps the background summary into msgs: summary + preserved
// tail from the snapshot, then everything appended since. Waits for a summary
// still in flight (cheaper than starting over). ok=false means the caller
// should summarize inline.
func (e *Engine) takeSpecCompact(ctx context.Context, msgs []types.Message) ([]types.Message, bool) {
	s := e.run.spec
	if s == nil {
		return nil, false
	}
	e.run.spec = nil
	start := time.Now()
	select {
	case <-s.done:
	case <-ctx.Done():
		s.cancel()
		return nil, false
	}
	s.cancel()
	if s.err != nil || !s.matches(msgs) || s.stats.AfterMsgs >= s.stats.BeforeMsgs {
		e.run.specFailed = true
		return nil, false
	}
	e.persistCheckpoint(ctx, s.out, s.stats)
	merged := append(append([]types.Message(nil), s.out...), msgs[s.snapLen:]...)
	stats := CompactStats{
		BeforeMsgs:   len(msgs),
		AfterMsgs:    len(merged),
		BeforeTokens: estimateMessagesTokens(msgs),
		AfterTokens:  estimateMessagesTokens(merged),
		Duration:     time.Since(start),
	}
	return e.emitCompactNotice(merged, stats), true
}

// cancelSpecCompact stops a background summary the Run no longer needs.
func (e *Engine) cancelSpecCompact() {
	if e.run != nil && e.run.spec != nil {
		e.run.spec.cancel()
		e.run.spec = nil
	}
}
