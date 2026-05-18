package loop

import (
	"context"
	"fmt"
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
	ch, err := e.Provider.Stream(ctx, req)
	if err != nil {
		// pre-stream HTTP errors already exhaust the provider's own retry
		// budget — surface as terminal, no further retry.
		return types.Message{}, "", nil, false, false, fmt.Errorf("provider stream: %w", err)
	}
	gotContent := false
	for ev := range ch {
		if ctx.Err() != nil {
			return types.Message{}, "", nil, false, false, ctx.Err()
		}
		switch ev.Type {
		case llm.EventThinkingDelta:
			thinkBuf.WriteString(ev.Delta)
			gotContent = true
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
				if !gotContent && isTransientStreamErr(ev.Err) {
					return types.Message{}, "", nil, false, true, ev.Err
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
				e.Costs.Record(e.Cfg.DefaultProvider, req.Model, ev.Usage.InputTokens, ev.Usage.OutputTokens)
			}
			if ev.Usage != nil && ev.Usage.InputTokens > 0 {
				e.lastInputTokens = ev.Usage.InputTokens
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
	// thinking block first so the rendered transcript reads in causal order
	if th := thinkBuf.String(); th != "" {
		*content = append(*content, types.ContentBlock{Type: types.BlockThinking, Text: th})
	}
	if t := textBuf.String(); t != "" {
		*content = append(*content, types.ContentBlock{Type: types.BlockText, Text: t})
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
	return msg, textBuf.String(), *toolUses, gotContent, false, nil
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
