package loop

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/cost"
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
	var (
		textBuf  strings.Builder
		thinkBuf strings.Builder
		content  []types.ContentBlock
		toolUses []types.ToolUse
	)
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return types.Message{}, "", nil, ctx.Err()
			case <-time.After(preContentRetryDelay):
			}
		}
		msg, finalText, uses, gotContent, retry, err := e.streamAttempt(ctx, req, &textBuf, &thinkBuf, &content, &toolUses)
		if retry && !gotContent && attempt < maxPreContentRetries {
			e.warnf("stream hiccup (%v) — retrying %d/%d", err, attempt+1, maxPreContentRetries)
			textBuf.Reset()
			thinkBuf.Reset()
			content = content[:0]
			toolUses = toolUses[:0]
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
	select {
	case e.WarnCh <- fmt.Sprintf(format, args...):
	default:
	}
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

// streamAttempt runs one Provider.Stream pass into the supplied buffers.
// Returns (msg, finalText, toolUses, gotContent, retryable, err). When
// retryable is true and gotContent is false, the caller may reopen the stream.
func (e *Engine) streamAttempt(
	ctx context.Context,
	req llm.Request,
	textBuf, thinkBuf *strings.Builder,
	content *[]types.ContentBlock,
	toolUses *[]types.ToolUse,
) (types.Message, string, []types.ToolUse, bool, bool, error) {
	// child context so the repetition watchdog can cut a wedged stream without
	// disturbing the caller's ctx (which still governs real cancellation).
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch, err := e.Provider.Stream(sctx, req)
	if err != nil {
		// pre-stream HTTP errors already exhaust the provider's own retry
		// budget — surface as terminal, no further retry.
		return types.Message{}, "", nil, false, false, fmt.Errorf("provider stream: %w", err)
	}
	gotContent := false
	sinceScan := 0 // bytes appended since the last repetition check
	looped := false
	truncated := false // stream dropped mid-output on a transient error
	loopPeriodText, loopPeriodThink := 0, 0
	loopTrimText, loopTrimThink := -1, -1
events:
	for ev := range ch {
		if ctx.Err() != nil {
			return types.Message{}, "", nil, false, false, ctx.Err()
		}
		switch ev.Type {
		case llm.EventThinkingDelta:
			thinkBuf.WriteString(ev.Delta)
			gotContent = true
			sinceScan += len(ev.Delta)
			if e.JSONEmitter != nil {
				e.JSONEmitter.Emit(jsonmode.Event{Type: "thinking", Delta: ev.Delta})
			} else if e.ThinkCh != nil {
				select {
				case e.ThinkCh <- ev.Delta:
				default:
				}
			}
		case llm.EventTextDelta:
			textBuf.WriteString(ev.Delta)
			gotContent = true
			sinceScan += len(ev.Delta)
			if e.JSONEmitter != nil {
				e.JSONEmitter.Emit(jsonmode.Event{Type: "text", Delta: ev.Delta})
			} else if e.StreamCh != nil {
				select {
				case e.StreamCh <- ev.Delta:
				default:
				}
			} else {
				_, _ = e.Stdout.Write([]byte(ev.Delta))
			}
		case llm.EventProgress:
			// withheld-output liveness from buffered tool-call modes. Not
			// content (no gotContent): the wrapper re-emits the parsed text
			// at end-of-stream, and pre-content retry must stay replayable.
			if e.ProgressCh != nil {
				select {
				case e.ProgressCh <- ev.N:
				default:
				}
			}
		case llm.EventToolUse:
			if ev.ToolUse != nil {
				*toolUses = append(*toolUses, *ev.ToolUse)
				gotContent = true
				if e.JSONEmitter != nil {
					e.JSONEmitter.Emit(jsonmode.Event{
						Type:  "tool_use",
						Name:  ev.ToolUse.Name,
						UseID: ev.ToolUse.ID,
						Input: ev.ToolUse.Input,
					})
				}
			}
		case llm.EventError:
			if ev.Err != nil {
				// drain remaining events so the provider goroutine exits cleanly
				for range ch {
				}
				if isTransientStreamErr(ev.Err) {
					if !gotContent {
						// pre-content hiccup: safe to replay the whole request.
						return types.Message{}, "", nil, false, true, ev.Err
					}
					// mid-content drop: replaying would duplicate the tokens
					// already streamed, so salvage the partial turn and let
					// turn_run nudge the model to continue from here.
					e.warnf("stream dropped mid-output (%v) — keeping partial turn, continuing", ev.Err)
					truncated = true
					break events
				}
				if e.JSONEmitter != nil {
					e.JSONEmitter.Emit(jsonmode.Event{Type: "error", Message: ev.Err.Error()})
				}
				return types.Message{}, "", nil, gotContent, false, ev.Err
			}
		case llm.EventDone:
			if e.JSONEmitter != nil {
				u := &jsonmode.Usage{}
				if ev.Usage != nil {
					u.Input = ev.Usage.InputTokens
					u.Output = ev.Usage.OutputTokens
				}
				e.JSONEmitter.Emit(jsonmode.Event{Type: "done", Usage: u})
			}
			if e.Costs != nil && ev.Usage != nil {
				cev := e.Costs.Record(e.Cfg.DefaultProvider, req.Model, ev.Usage.InputTokens, ev.Usage.OutputTokens)
				// prefer provider-reported spend over the static estimate.
				usd, reported := cev.USD, false
				if ev.Usage.CostUSD > 0 {
					usd, reported = ev.Usage.CostUSD, true
				}
				cost.AppendUsage(cost.UsageRecord{
					Time:         cev.Time,
					Provider:     e.Cfg.DefaultProvider,
					Model:        req.Model,
					Input:        ev.Usage.InputTokens,
					Output:       ev.Usage.OutputTokens,
					Cached:       ev.Usage.CachedTokens,
					USD:          usd,
					CostReported: reported,
				})
			}
			// Bump persisted lifetime totals so the splash banner can show
			// "1.2M tok" across all bee sessions ever. Separate from the
			// process-local Costs tracker (which resets on /new), and
			// hooked here so unit tests using cost.New() directly stay
			// hermetic — production has exactly one place tokens enter.
			if ev.Usage != nil {
				cost.AddLifetime(ev.Usage.InputTokens, ev.Usage.OutputTokens)
			}
			if ev.Usage != nil && ev.Usage.InputTokens > 0 {
				e.run.lastInputTokens = ev.Usage.InputTokens
			}
			// cumulative spend feeds the adaptive token-budget cap in
			// turn_run. tracked separately from lastInputTokens (which is
			// a per-request snapshot, not a sum).
			if ev.Usage != nil {
				e.run.cumInputTokens += ev.Usage.InputTokens
				e.run.cumOutputTokens += ev.Usage.OutputTokens
			}
		}
		// repetition watchdog: a wedged local model can loop the same phrase
		// until max_tokens, so the stream never closes and the turn can't make
		// progress. detect a periodic tail and cut the stream — turn_run then
		// nudges, or bails after loopCutBailAt consecutive cuts.
		if sinceScan >= loopScanStride {
			sinceScan = 0
			if p := degenerateTailPeriod(textBuf.String()); p > 0 {
				loopPeriodText, looped = p, true
			} else if p := degenerateTailPeriod(thinkBuf.String()); p > 0 {
				loopPeriodThink, looped = p, true
			} else if off := degenerateLowVocabTail(textBuf.String()); off >= 0 {
				loopTrimText, looped = off, true
			} else if off := degenerateLowVocabTail(thinkBuf.String()); off >= 0 {
				loopTrimThink, looped = off, true
			} else if off := degenerateBlockTail(textBuf.String()); off >= 0 {
				loopTrimText, looped = off, true
			} else if off := degenerateBlockTail(thinkBuf.String()); off >= 0 {
				loopTrimThink, looped = off, true
			}
			if looped {
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
		return types.Message{}, "", nil, gotContent, false, ctx.Err()
	}
	thinkStr, textStr := thinkBuf.String(), textBuf.String()
	if looped {
		// collapse the repeated tail so it doesn't bloat the transcript, and
		// flag the turn so turn_run nudges/bails instead of finishing on garbage.
		// exact-period loops trim by period; low-vocab loops trim by offset.
		if loopPeriodThink > 0 {
			thinkStr = trimLoopedTail(thinkStr, loopPeriodThink)
		} else if loopTrimThink >= 0 {
			thinkStr = trimLoopedTailAt(thinkStr, loopTrimThink)
		}
		if loopPeriodText > 0 {
			textStr = trimLoopedTail(textStr, loopPeriodText)
		} else if loopTrimText >= 0 {
			textStr = trimLoopedTailAt(textStr, loopTrimText)
		}
		e.run.lastTurnLooped = true
	}
	if truncated {
		e.run.lastTurnTruncated = true
	}
	// thinking block first so the rendered transcript reads in causal order
	if thinkStr != "" {
		*content = append(*content, types.ContentBlock{Type: types.BlockThinking, Text: thinkStr})
	}
	if textStr != "" {
		*content = append(*content, types.ContentBlock{Type: types.BlockText, Text: textStr})
	}
	for i := range *toolUses {
		tu := (*toolUses)[i]
		*content = append(*content, types.ContentBlock{Type: types.BlockToolUse, Use: &tu})
	}
	msg := types.Message{
		ID:      newID(),
		Role:    types.RoleAssistant,
		Content: *content,
		Time:    time.Now().UTC(),
	}
	return msg, textStr, *toolUses, gotContent, false, nil
}

// isTransientStreamErr returns true for momentary network / provider hiccups.
// Safe to retry only before any content was emitted — caller's responsibility.
func isTransientStreamErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, m := range []string{
		"sse scan",
		"stream stalled",
		"context deadline",
		"Client.Timeout",
		"EOF",
		"connection reset",
		"broken pipe",
		"i/o timeout",
		"use of closed network",
	} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}
