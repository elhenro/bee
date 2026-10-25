package loop

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/jsonmode"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// maxPreContentRetries caps the reopen budget when the provider fails before
// emitting any content. Beyond this we surface the error rather than risk a
// stuck retry loop.
const maxPreContentRetries = 2

// preContentRetryDelay is the gap before re-opening the stream after a
// pre-content failure. Var, not const, so tests can shrink it.
var preContentRetryDelay = 800 * time.Millisecond

// streamOnce drains one provider stream into a single assistant message.
// On pre-content transient errors it reopens up to maxPreContentRetries times
// and emits a WarnCh notice per retry.
func (e *Engine) streamOnce(ctx context.Context, req llm.Request) (types.Message, string, []types.ToolUse, error) {
	e.earlyDrain() // nothing from a turn that never reached dispatch
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return types.Message{}, "", nil, ctx.Err()
			case <-time.After(preContentRetryDelay):
			}
		}
		msg, finalText, uses, retry, err := e.streamAttempt(ctx, req)
		if retry && attempt < maxPreContentRetries {
			e.warnf("stream hiccup (%v) — retrying %d/%d", err, attempt+1, maxPreContentRetries)
			continue
		}
		return msg, finalText, uses, err
	}
}

// warnf sends a transient warning to WarnCh if wired. Non-blocking — a slow
// consumer drops the notice rather than stalling the loop.
func (e *Engine) warnf(format string, args ...any) {
	if e == nil || e.WarnCh == nil {
		return
	}
	trySend(e.WarnCh, fmt.Sprintf(format, args...))
}

// noticef is warnf with a headless fallback: when no WarnCh consumer is wired
// (bee run, zzz), the notice goes to stderr instead of being dropped.
func (e *Engine) noticef(format string, args ...any) {
	if e != nil && e.WarnCh != nil {
		e.warnf(format, args...)
		return
	}
	fmt.Fprintf(os.Stderr, "bee: "+format+"\n", args...)
}

// turnStream accumulates one provider stream into an assistant turn.
type turnStream struct {
	text, think strings.Builder
	sig         string // signature of the thinking block
	uses        []types.ToolUse
	clock       thinkClock
	got         bool // any content arrived, so replaying the request would duplicate it
	sinceScan   int  // bytes appended since the last repetition check
}

// streamAttempt runs one Provider.Stream pass. retry is true when the stream
// failed before any content, so the caller may reopen it.
func (e *Engine) streamAttempt(ctx context.Context, req llm.Request) (types.Message, string, []types.ToolUse, bool, error) {
	// child context so the repetition watchdog can cut a wedged stream without
	// disturbing the caller's ctx (which still governs real cancellation).
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	e.gaugeRequest()
	ch, err := e.Provider.Stream(sctx, req)
	if err != nil {
		// pre-stream HTTP errors already exhaust the provider's own retry
		// budget — surface as terminal, no further retry. distinguish
		// provider-reject (HTTP 400 on the request body) from a transient
		// transport failure: only the former is wedge-class, since the same
		// transcript will keep 400-ing until the bad tool_use is removed.
		if isProviderRejectErr(err) {
			return types.Message{}, "", nil, false, &ProviderRejectError{Raw: err.Error()}
		}
		return types.Message{}, "", nil, false, fmt.Errorf("provider stream: %w", err)
	}
	var (
		s         turnStream
		trim      func(string) string // collapses the looped tail of the cut buffer
		trimThink bool                // ...which is the reasoning one
		truncated bool                // stream dropped mid-output on a transient error
	)
events:
	for ev := range ch {
		if ctx.Err() != nil {
			return types.Message{}, "", nil, false, ctx.Err()
		}
		switch ev.Type {
		case llm.EventThinkingDelta:
			s.reasoning(e, ev)
		case llm.EventTextDelta:
			s.answer(e, ev.Delta)
		case llm.EventProgress:
			// withheld-output liveness from buffered tool-call modes. Not
			// content (no got): the wrapper re-emits the parsed text
			// at end-of-stream, and pre-content retry must stay replayable.
			trySend(e.ProgressCh, ev.N)
		case llm.EventToolUse:
			if ev.ToolUse != nil {
				s.toolUse(ctx, e, *ev.ToolUse)
			}
		case llm.EventError:
			if ev.Err == nil {
				break
			}
			// drain remaining events so the provider goroutine exits cleanly
			for range ch {
			}
			if isTransientStreamErr(ev.Err) {
				if !s.got {
					// pre-content hiccup: safe to replay the whole request.
					return types.Message{}, "", nil, true, ev.Err
				}
				// mid-content drop: replaying would duplicate the tokens
				// already streamed, so salvage the partial turn and let
				// the run loop nudge the model to continue from here.
				e.warnf("stream dropped mid-output (%v) — keeping partial turn, continuing", ev.Err)
				truncated = true
				break events
			}
			if e.JSONEmitter != nil {
				e.JSONEmitter.Emit(jsonmode.Event{Type: "error", Message: ev.Err.Error()})
			}
			return types.Message{}, "", nil, false, ev.Err
		case llm.EventDone:
			e.recordDone(req.Model, ev.Usage)
		}
		// repetition watchdog: a wedged local model can loop the same phrase
		// until max_tokens, so the stream never closes and the turn can't make
		// progress. detect a periodic tail and cut the stream — the run loop then
		// nudges, or bails after loopCutBailAt consecutive cuts.
		if s.sinceScan >= loopScanStride {
			s.sinceScan = 0
			if trim, trimThink = scanLoop(s.text.String(), s.think.String()); trim != nil {
				e.warnf("output stuck repeating — cut the stream")
				cancel()
				for range ch { // drain so the provider goroutine exits cleanly
				}
				break
			}
		}
	}
	// post-stream ctx check: if the channel closed without an explicit
	// terminal event because the caller canceled, surface that as the err
	// rather than returning a half-formed message with nil. otherwise
	// races between cancel() and the provider goroutine exit can swallow
	// the cancellation entirely.
	if ctx.Err() != nil {
		return types.Message{}, "", nil, false, ctx.Err()
	}
	thinkStr, textStr := s.think.String(), s.text.String()
	if trim != nil {
		// collapse the repeated tail so it doesn't bloat the transcript, and
		// flag the turn so the run loop nudges/bails instead of finishing on garbage.
		if trimThink {
			thinkStr = trim(thinkStr)
			s.sig = "" // edited text no longer matches its signature
		} else {
			textStr = trim(textStr)
		}
		e.run.lastTurnLooped = true
	}
	if truncated {
		e.run.lastTurnTruncated = true
	}
	var content []types.ContentBlock
	// thinking block first so the rendered transcript reads in causal order
	if thinkStr != "" || s.sig != "" {
		content = append(content, types.ContentBlock{Type: types.BlockThinking, Text: thinkStr, Signature: s.sig, ThinkMs: s.clock.ms()})
	}
	if textStr != "" {
		content = append(content, textBlock(textStr))
	}
	for _, tu := range s.uses {
		content = append(content, types.ContentBlock{Type: types.BlockToolUse, Use: &tu})
	}
	return newMessage(types.RoleAssistant, "", content...), textStr, s.uses, false, nil
}

func (s *turnStream) reasoning(e *Engine, ev llm.Event) {
	s.clock.reasoning()
	if ev.Signature != "" {
		s.sig = ev.Signature
	}
	s.think.WriteString(ev.Delta)
	s.got = true
	s.sinceScan += len(ev.Delta)
	if e.JSONEmitter != nil {
		e.JSONEmitter.Emit(jsonmode.Event{Type: "thinking", Delta: ev.Delta})
	} else {
		trySend(e.ThinkCh, ev.Delta)
	}
}

func (s *turnStream) answer(e *Engine, delta string) {
	s.clock.answered()
	s.text.WriteString(delta)
	s.got = true
	s.sinceScan += len(delta)
	switch {
	case e.JSONEmitter != nil:
		e.JSONEmitter.Emit(jsonmode.Event{Type: "text", Delta: delta})
	case e.StreamCh != nil:
		trySend(e.StreamCh, delta)
	default:
		_, _ = e.Stdout.Write([]byte(delta))
	}
}

func (s *turnStream) toolUse(ctx context.Context, e *Engine, u types.ToolUse) {
	s.clock.answered()
	s.uses = append(s.uses, u)
	e.earlyOffer(ctx, len(s.uses)-1, u)
	s.got = true
	if e.JSONEmitter != nil {
		e.JSONEmitter.Emit(jsonmode.Event{Type: "tool_use", Name: u.Name, UseID: u.ID, Input: u.Input})
	}
}
