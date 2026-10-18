package loop

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/safety"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
	"github.com/elhenro/bee/internal/waggle"
)

// observeWaggle feeds the forage log so repeated read-only routes can
// crystallize into waggles. runs once per batch, post-flush, in input order:
// parallel goroutines finish in arbitrary order, which would scramble the
// route the miner sees. only successful calls; bash is marked mutating
// (ambiguous) and so never crystallizes. best-effort, never affects the turn.
func (e *Engine) observeWaggle(uses []types.ToolUse, results []types.ToolResult) {
	if e.Waggle == nil {
		return
	}
	for i, u := range uses {
		if results[i].IsError {
			continue
		}
		e.Waggle.Observe(waggle.Call{
			Tool:      u.Name,
			Args:      stringifyArgs(u.Args()),
			Mutates:   !safeParallelTools[u.Name],
			EstTokens: len(results[i].Content) / 4,
		})
	}
}

// replayFollow feeds this batch's successful read-only calls to the predictive
// replayer in input order (the parallel dispatch path can't preserve order, so
// this runs once, post-flush) and, on a confident prefix match, runs the route's
// remaining steps and appends the combined output to the call that completed the
// recognized prefix. Best-effort: never errors the turn.
func (e *Engine) replayFollow(ctx context.Context, uses []types.ToolUse, results []types.ToolResult) {
	if e.Replay == nil {
		return
	}
	last := -1
	for i, u := range uses {
		if results[i].IsError || !safeParallelTools[u.Name] {
			continue
		}
		e.Replay.Observe(waggle.Call{Tool: u.Name, Args: stringifyArgs(u.Args())})
		last = i
	}
	if last < 0 {
		return
	}
	if block, ok := e.Replay.Follow(ctx, e.replayExec); ok {
		results[last].Content += block
	}
}

// replayExec runs a fully-literal read-only script for the replayer through the
// host shell tool, so sandbox wrapping, secret redaction and per-tool truncation
// apply exactly as for a model-issued call. A hardline safety re-check guards
// against a recorded route that resolves to something dangerous later.
func (e *Engine) replayExec(ctx context.Context, script string) (string, error) {
	if e.Tools == nil {
		return "", errors.New("replay: tools unavailable")
	}
	if err := safety.CheckShellCommand(script); err != nil {
		return "", err
	}
	t, ok := e.Tools.Get("bash")
	if !ok {
		return "", errors.New("replay: shell tool unavailable")
	}
	out, err := t.Run(ctx, wrapShellInput(map[string]any{"command": script}, e.Cfg.Sandbox, e.Cwd))
	if err != nil {
		return "", err
	}
	if out.IsError {
		return "", fmt.Errorf("replay step failed: %s", out.Content)
	}
	content := out.Content
	if e.Cfg.Sandbox.Confined() {
		content = safety.Redact(content)
	}
	content, _ = tools.TruncateWithLimit("bash", content, config.ActiveProfile(e.Cfg).ToolOutputTokens)
	return content, nil
}

// stringifyArgs flattens a tool's input to string values for the forage log,
// dropping the loop's internal underscore-prefixed keys (parse diagnostics,
// sandbox-wrapped command originals).
func stringifyArgs(in map[string]any) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if strings.HasPrefix(k, "_") {
			continue
		}
		if s, ok := v.(string); ok {
			out[k] = s
		} else {
			out[k] = fmt.Sprintf("%v", v)
		}
	}
	return out
}
