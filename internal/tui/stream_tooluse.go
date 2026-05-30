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
	head := r.styles.WarnBadge.Render("escalate") + " " + r.styles.Dim.Render("needs you")
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return head + "\n"
	}
	rail := r.styles.WarnRail.Render("▎")
	width := r.width - 2
	if width < 20 {
		width = 20
	}
	out := []string{head}
	for _, ln := range wrapHanging(reason, width) {
		out = append(out, rail+" "+r.styles.Warn.Render(ln))
	}
	if next = strings.TrimSpace(next); next != "" {
		first := true
		for _, ln := range wrapHanging(next, width-2) {
			arrow := "  "
			if first {
				arrow = r.styles.Dim.Render("→ ")
				first = false
			}
			out = append(out, rail+" "+arrow+r.styles.Dim.Render(ln))
		}
	}
	return strings.Join(out, "\n") + "\n"
}

// renderToolUse renders the compact card: name  args-summary. Lilac
// name so tool activity reads as a distinct lane vs honey-AI prose and
// blue-user input. Args are omitted entirely (no trailing whitespace)
// when the tool was called with no input.
//
// File-mutation tools (edit / hashline_edit / apply_patch / write) skip
// the json summary and instead drop a colored diff card under the header
// so the reader sees the change itself, not `{"new":"...","old":"..."}`.
func (r *StreamRenderer) renderToolUse(u types.ToolUse) string {
	if u.Name == "escalate" {
		return r.renderEscalate(u)
	}
	name := r.styles.ToolName.Render(u.Name)
	if header, body, ok := r.renderEditPreview(u); ok {
		head := fmt.Sprintf("%s  %s", name, header)
		if body == "" {
			return head + "\n"
		}
		return head + "\n" + body + "\n"
	}
	args := summarizeToolArgs(u.Name, u.Input, r.argsBudget(u.Name))
	if args == "" {
		return fmt.Sprintf("%s\n", name)
	}
	if u.Name == "bash" && r.highlight {
		return fmt.Sprintf("%s  %s\n", name, r.hlLang(args, "bash"))
	}
	return fmt.Sprintf("%s  %s\n", name, r.styles.ToolArgs.Render(args))
}

// summarizeToolArgs routes per-tool pretty summaries before falling back to
// the generic json/single-key path. bash gets its `command` pulled out bare
// (tool name already says "bash", so `command:` prefix would be redundant)
// with cwd folded in as a compact `· <cwd>` suffix when set.
func summarizeToolArgs(toolName string, in map[string]any, budget int) string {
	switch toolName {
	case "search":
		if s, ok := summarizeSearch(in, budget); ok {
			return s
		}
	case "glob":
		if s, ok := summarizeGlob(in, budget); ok {
			return s
		}
	}
	if toolName == "bash" {
		if s, ok := summarizeBash(in, budget); ok {
			return s
		}
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
	out := pat
	if p, ok := in["path"].(string); ok && p != "" {
		out += "  · " + shortenPath(p)
	}
	if g, ok := in["glob"].(string); ok && g != "" {
		out += "  · *." + g
	}
	if c, ok := in["context"]; ok {
		if n, _ := c.(float64); n > 0 {
			out += fmt.Sprintf("  · ctx %d", int(n))
		}
	}
	if co, ok := in["count_only"].(bool); ok && co {
		out += "  · count"
	}
	return truncateRunes(out, budget), true
}

// summarizeGlob renders a glob tool call with the filename pattern leading.
// Optional `path` trails as `· <path>`. Returns (_, false) when name is
// missing so the caller falls back to the generic summary.
func summarizeGlob(in map[string]any, budget int) (string, bool) {
	name, ok := in["name"].(string)
	if !ok || name == "" {
		return "", false
	}
	out := name
	if p, ok := in["path"].(string); ok && p != "" {
		out += "  · " + shortenPath(p)
	}
	return truncateRunes(out, budget), true
}
