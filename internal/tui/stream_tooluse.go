package tui

import (
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// renderEscalate draws the escalate card: a mustard "escalate" pill + a quiet
// "needs you" tag, then the model's reason wrapped under a warn rail and the
// suggested next action on a `→` line. The escalate tool isn't a crash — it's
// the model handing control back — so it gets the warn palette, not red error
// styling, and the textual tool_result is suppressed (renderToolResult) so the
// word "escalate" shows exactly once.
func (r *StreamRenderer) renderEscalate(u types.ToolUse) string {
	reason, _ := u.Input["reason"].(string)
	next, _ := u.Input["suggested_next_action"].(string)
	// when the model supplied discrete options, the interactive picker renders
	// them below — keep them out of the static card so they don't show twice.
	if opts, ok := u.Input["options"].([]any); ok && len(opts) > 0 {
		next = ""
	}
	head := r.styles.WarnBadge.Render("escalate") + " " + r.styles.Dim.Render("needs you")
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return head + "\n"
	}
	rail := r.styles.WarnRail.Render("▎")
	width := max(r.width-2, 20)
	out := []string{head}
	for _, ln := range wrapHanging(reason, width) {
		out = append(out, rail+" "+r.styles.Warn.Render(ln))
	}
	if next = strings.TrimSpace(next); next != "" {
		for i, ln := range wrapHanging(next, width-2) {
			arrow := "  "
			if i == 0 {
				arrow = r.styles.Dim.Render("→ ")
			}
			out = append(out, rail+" "+arrow+r.styles.Dim.Render(ln))
		}
	}
	return strings.Join(out, "\n") + "\n"
}

// summarizeToolArgs routes per-tool pretty summaries before falling back to
// the generic json/single-key path. bash gets its `command` pulled out bare
// (tool name already says "bash", so `command:` prefix would be redundant)
// with cwd folded in as a compact `· <cwd>` suffix when set.
func summarizeToolArgs(toolName string, in map[string]any, budget int) string {
	var s string
	var ok bool
	switch toolName {
	case "search":
		s, ok = summarizeSearch(in, budget)
	case "glob":
		s, ok = summarizeGlob(in, budget)
	case "bash":
		s, ok = summarizeBash(in, budget)
	}
	if ok {
		return s
	}
	return summarizeArgs(in, budget)
}

// summarizeBash renders a bash tool call as `<command> · <cwd>` (cwd
// suffix dropped when empty). Command paths shorten inline so cd targets
// don't waste card width on /Users/<name>/projects/... prefixes. Returns
// (_, false) when the input has no `command` field so the caller falls
// back to the generic json summary path.
func summarizeBash(in map[string]any, budget int) (string, bool) {
	cmd, ok := in["command"].(string)
	if !ok || cmd == "" {
		return "", false
	}
	out := shortenPathsInline(cmd)
	if cwd, ok := in["cwd"].(string); ok && cwd != "" {
		out += "  · " + shortenPath(cwd)
	}
	return truncateRunes(out, budget), true
}

// summarizeSearch renders a search tool call with the regex pattern leading
// the card — that's the primary signal. Optional `path`, `glob`, `context`,
// `count_only` trail as compact `· key: val` suffixes when present and
// non-default. Returns (_, false) when pattern is missing so the caller
// falls back to the generic json summary path.
func summarizeSearch(in map[string]any, budget int) (string, bool) {
	pat, ok := in["pattern"].(string)
	if !ok || pat == "" {
		return "", false
	}
	out := pat + scopeSuffix(in)
	if g, _ := in["glob"].(string); g != "" {
		out += "  · *." + g
	}
	if n, _ := in["context"].(float64); n > 0 {
		out += fmt.Sprintf("  · ctx %d", int(n))
	}
	if co, _ := in["count_only"].(bool); co {
		out += "  · count"
	}
	return truncateRunes(out, budget), true
}

// `  · <path>` when the call narrows the search; the workspace root is the
// default scope, so naming it adds only noise
func scopeSuffix(in map[string]any) string {
	p, _ := in["path"].(string)
	if p == "" || p == "." || p == "./" {
		return ""
	}
	return "  · " + shortenPath(p)
}

// summarizeGlob renders a glob tool call with the filename pattern leading.
// Optional `path` trails as `· <path>`. Returns (_, false) when no pattern is
// given so the caller falls back to the generic summary. `name` is the
// legacy spelling of `pattern`.
func summarizeGlob(in map[string]any, budget int) (string, bool) {
	name, _ := in["pattern"].(string)
	if name == "" {
		name, _ = in["name"].(string)
	}
	if name == "" {
		return "", false
	}
	return truncateRunes(name+scopeSuffix(in), budget), true
}
