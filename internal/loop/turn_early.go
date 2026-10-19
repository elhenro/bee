package loop

import (
	"context"
	"sync"

	"github.com/elhenro/bee/internal/types"
)

// early dispatch: a read-only call starts the moment its tool_use arrives, while
// the model is still writing its siblings. dispatchTools then collects the
// result instead of running the call, so a slow search overlaps the rest of the
// stream.
//
// Only the leading run of safeParallelTools calls qualifies. The first other call
// (shell, edit, write) closes early dispatch for the turn: a read emitted after
// it must still observe its effects, and a mutator must not run before the whole
// turn is known to be kept (a failed stream drops the assistant message).

type earlyCall struct {
	use  types.ToolUse
	done chan struct{}
	res  types.ToolResult
}

type earlyState struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	calls  []*earlyCall // by position in the turn's tool_use order; nil = not early
	closed bool         // a non-qualifying call was seen
}

// earlyOffer is called for each tool_use as the stream delivers it. idx is the
// call's position in the turn.
func (e *Engine) earlyOffer(ctx context.Context, idx int, u types.ToolUse) {
	if e.run == nil {
		return
	}
	st := e.run.early
	if st == nil {
		st = &earlyState{}
		st.ctx, st.cancel = context.WithCancel(ctx)
		e.run.early = st
	}
	if st.closed {
		return
	}
	if !safeParallelTools[u.Name] || idx != len(st.calls) {
		st.closed = true
		return
	}
	if pe, _ := u.Input["_parse_error"].(string); pe != "" {
		st.closed = true
		return
	}
	c := &earlyCall{use: u, done: make(chan struct{})}
	st.calls = append(st.calls, c)
	st.wg.Add(1)
	go func() {
		defer st.wg.Done()
		defer close(c.done)
		c.res = e.runOneTrapped(st.ctx, u)
	}()
}

// earlyTake returns the early run of call i when it is the same call dispatch
// is about to execute, else nil.
func (e *Engine) earlyTake(i int, u types.ToolUse) *earlyCall {
	if e.run == nil || e.run.early == nil || i >= len(e.run.early.calls) {
		return nil
	}
	c := e.run.early.calls[i]
	if c == nil || c.use.ID != u.ID || c.use.Name != u.Name {
		return nil
	}
	e.run.early.calls[i] = nil
	return c
}

// earlyDrain ends the turn's early calls: cancels what still runs and waits, so
// no goroutine outlives the dispatch (or a Run that failed before dispatch).
func (e *Engine) earlyDrain() {
	if e.run == nil || e.run.early == nil {
		return
	}
	st := e.run.early
	e.run.early = nil
	st.cancel()
	st.wg.Wait()
}
