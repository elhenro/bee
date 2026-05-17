package tui

import (
	"fmt"

	"github.com/elhenro/bee/internal/types"
)

// renderToolUse renders the compact card: name  args-summary. Lilac
// name so tool activity reads as a distinct lane vs honey-AI prose and
// blue-user input. Args are omitted entirely (no trailing whitespace)
// when the tool was called with no input.
//
// File-mutation tools (edit / hashline_edit / apply_patch / write) skip
// the json summary and instead drop a colored diff card under the header
// so the reader sees the change itself, not `{"new":"...","old":"..."}`.
func (r *StreamRenderer) renderToolUse(u types.ToolUse) string {
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
