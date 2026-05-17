package loop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/jsonmode"
	"github.com/elhenro/bee/internal/safety"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// safeParallelTools lists read-only tools that can run concurrently within
// one turn. Mutators (shell, apply_patch, edit_diff, hashline_edit, write)
// stay serial to preserve happens-before and avoid sandbox contention.
var safeParallelTools = map[string]bool{
	"read":          true,
	"search":        true,
	"glob":          true,
	"ls":            true,
	"knowledge_search": true,
}

// dispatchTools runs read-only tools concurrently and mutators serially.
// Order in the returned slice matches the input order so UseIDs line up
// with the original ToolUse blocks. A serial tool acts as a barrier:
// all in-flight parallel tools complete before it runs, and nothing starts
// after it until it finishes. ctx cancellation short-circuits.
func (e *Engine) dispatchTools(ctx context.Context, uses []types.ToolUse) ([]types.ToolResult, error) {
	results := make([]types.ToolResult, len(uses))
	var wg sync.WaitGroup
	flush := func() { wg.Wait() }
	for i, u := range uses {
		if ctx.Err() != nil {
			flush()
			return results, ctx.Err()
		}
		if safeParallelTools[u.Name] {
			wg.Add(1)
			go func(idx int, use types.ToolUse) {
				defer wg.Done()
				results[idx] = e.runOneTrapped(ctx, use)
			}(i, u)
			continue
		}
		// mutator / shell — drain pending parallel work first, run serially
		flush()
		if ctx.Err() != nil {
			return results, ctx.Err()
		}
		results[i] = e.runOneTrapped(ctx, u)
	}
	flush()
	for _, r := range results {
		if e.JSONEmitter != nil {
			e.JSONEmitter.Emit(jsonmode.Event{
				Type:    "tool_result",
				UseID:   r.UseID,
				Content: r.Content,
				Error:   r.IsError,
			})
		}
	}
	return results, nil
}

// runOneTrapped wraps runOne so ctx cancel propagates while ordinary tool
// errors are folded into a ToolResult the model can react to. Used by the
// parallel dispatcher where caller can't directly return an err.
func (e *Engine) runOneTrapped(ctx context.Context, u types.ToolUse) types.ToolResult {
	res, err := e.runOne(ctx, u)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return types.ToolResult{UseID: u.ID, Content: err.Error(), IsError: true}
		}
		return types.ToolResult{
			UseID:   u.ID,
			Content: fmt.Sprintf("tool error: %v", err),
			IsError: true,
		}
	}
	return res
}

func (e *Engine) runOne(ctx context.Context, u types.ToolUse) (types.ToolResult, error) {
	if e.Tools == nil {
		return types.ToolResult{UseID: u.ID, Content: "no tools registered", IsError: true}, nil
	}
	// short-circuit on upstream parse failure so the model sees a clean
	// diagnostic (with the raw args) instead of the tool running on `{}` and
	// returning a generic "missing field" error.
	if pe, ok := u.Input["_parse_error"].(string); ok && pe != "" {
		raw, _ := u.Input["_raw_args"].(string)
		msg := fmt.Sprintf("tool args failed to parse: %s\nemit args as a single JSON object, no extra markup or trailing tokens.", pe)
		if raw != "" {
			msg += fmt.Sprintf("\nraw=%q", truncForLog(raw, 240))
		}
		return types.ToolResult{UseID: u.ID, Content: msg, IsError: true}, nil
	}
	t, ok := e.Tools.Get(u.Name)
	if !ok {
		return types.ToolResult{UseID: u.ID, Content: unknownToolMsg(u.Name, e.Tools.Names()), IsError: true}, nil
	}
	input := u.Input
	if u.Name == "bash" {
		input = wrapShellInput(input, e.Cfg.Sandbox, e.Cwd)
	}
	out, err := t.Run(ctx, input)
	if err != nil {
		return types.ToolResult{}, err
	}
	// scrub obvious secrets before fold into model context. defense layer:
	// shell stdout / read output can carry env files, key dumps, etc.
	out.Content = safety.Redact(out.Content)
	content, truncated := tools.TruncateWithLimit(u.Name, out.Content, config.ActiveProfile(e.Cfg).ToolOutputTokens)
	if truncated && e.JSONEmitter != nil {
		e.JSONEmitter.Emit(jsonmode.Event{Type: "tool_truncated", Name: u.Name, UseID: u.ID})
	}
	return types.ToolResult{UseID: u.ID, Content: content, IsError: out.IsError}, nil
}

// unknownToolMsg builds a diagnostic the model can act on. It surfaces a
// likely-cause hint when the bad name looks like markup leakage (the
// deepseek-v4 "DSML" failure mode) so the model knows to re-emit a plain
// identifier, plus the actual available tool names.
func unknownToolMsg(bad string, available []string) string {
	hint := ""
	if looksLikeMarkupLeak(bad) {
		hint = "\nname contains chat-template markup — emit the tool name as a plain identifier in function.name, with all args inside function.arguments JSON."
	}
	list := strings.Join(available, ", ")
	return fmt.Sprintf("unknown tool %q.%s\navailable: %s", bad, hint, list)
}

// looksLikeMarkupLeak flags tool names that smuggle in chat-template tokens
// or arg syntax. used only for picking the hint above.
func looksLikeMarkupLeak(s string) bool {
	if strings.ContainsAny(s, "<>\"'\n ") {
		return true
	}
	if strings.Contains(s, "｜") {
		return true
	}
	return false
}

func truncForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
