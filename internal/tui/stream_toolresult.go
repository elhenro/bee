package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/elhenro/bee/internal/types"
)

// renderToolResult renders a tight preview of the output. Lilac left rail
// (▎) prefixes each preview line so the block visually groups under the
// tool call card. Errors get colored. Tool stdout is ANSI-stripped before
// display — raw escape sequences (cursor moves, clear-line) from subprocesses
// like `go test` would otherwise blit over the TUI chrome inside altscreen.
// Blank lines are dropped so the preview stays compact even when the
// underlying output is double-spaced. Overflow is signalled inline as
// `… +N more` on the last preview line rather than spending a row on a
// truncation footer.
func (r *StreamRenderer) renderToolResult(res types.ToolResult) string {
	// edit-family tools render a rich diff card via renderToolUse already
	// (path + `+adds −dels` header + colored body). The textual result
	// echoes "replaced N occurrence(s)" + hashline-anchored region listing
	// — useful to the LLM for chained edits without re-reads, redundant
	// noise for the human reader. Suppress on success; errors still surface.
	if !res.IsError {
		if use, ok := r.toolUses[res.UseID]; ok {
			switch use.Name {
			case "edit", "hashline_edit", "apply_patch", "write":
				return ""
			}
		}
	}
	// escalate renders its full card in renderToolUse (badge + reason + next
	// action); the textual result just repeats the reason, so drop it.
	if use, ok := r.toolUses[res.UseID]; ok && use.Name == "escalate" {
		return ""
	}
	clean := ansi.Strip(res.Content)
	totalBytes := len(clean)
	lines := compactLines(clean)
	if len(lines) == 0 {
		return ""
	}
	// refusal/denial: tool ran the safety/approval path and was blocked
	// rather than failing. Marker prefixes come from shell.go ("refused by
	// user:"), safety/shell.go + safety/paths.go ("refused: …"), and write
	// filters ("path … denied by write filter"). These read as "blocked,
	// not broken" — use the yellow warn palette so the eye distinguishes
	// them from real bash errors at a glance.
	isRefusal := res.IsError && len(lines) > 0 && (strings.HasPrefix(lines[0], "refused") || strings.Contains(lines[0], "denied by"))

	style := r.styles.ToolPrev
	switch {
	case isRefusal:
		style = r.styles.Warn
	case res.IsError:
		style = r.styles.Error
	}

	// bash error/refusal preview: replace the leading "exit N" or
	// "refused …" line with the originating command rendered on a colored
	// bg badge. Reads "this cmd failed/was blocked" at a glance instead of
	// forcing the user to scroll up and correlate the bare exit/refusal with
	// the originating tool call.
	var headerOverride string
	if res.IsError && len(lines) > 0 && strings.HasPrefix(lines[0], "exit ") {
		if use, ok := r.toolUses[res.UseID]; ok && use.Name == "bash" {
			if cmd, _ := use.Input["command"].(string); cmd != "" {
				exitTag := lines[0]
				lines = lines[1:]
				cmdRendered := r.styles.ErrorCmd.Render("$ " + shortenPathsInline(cmd))
				headerOverride = cmdRendered + " " + r.styles.Dim.Render("("+exitTag+")")
			}
		}
	} else if isRefusal {
		if use, ok := r.toolUses[res.UseID]; ok && use.Name == "bash" {
			if cmd, _ := use.Input["command"].(string); cmd != "" {
				cmdRendered := r.styles.WarnCmd.Render("$ " + shortenPathsInline(cmd))
				headerOverride = cmdRendered
			}
		}
	}

	hidden := 0
	cap := r.previewLines()
	if cap >= 0 && len(lines) > cap {
		hidden = len(lines) - cap
		lines = lines[:cap]
	}
	rail := r.styles.ToolRail.Render("▎")
	rendered := make([]string, 0, len(lines)+1)
	if headerOverride != "" {
		rendered = append(rendered, rail+" "+headerOverride)
	}
	// when source is a file read, run the whole payload through chroma once
	// (multi-line lexers like markdown/yaml need cross-line context to
	// classify tokens correctly) before splitting + re-shortening. Only
	// applies on success — errors keep the red error-style fallback.
	hlPath := ""
	if r.highlight && !res.IsError {
		if use, ok := r.toolUses[res.UseID]; ok && use.Name == "read" {
			if p, _ := use.Input["path"].(string); p != "" {
				hlPath = p
			}
		}
	}
	for _, ln := range lines {
		s := shortenPathsInline(ln)
		if hlPath != "" {
			rendered = append(rendered, rail+" "+r.hl(s, hlPath))
			continue
		}
		rendered = append(rendered, rail+" "+style.Render(s))
	}
	out := strings.Join(rendered, "\n")
	switch {
	case hidden > 0:
		// surface both hidden-row count *and* total payload size — `+1 more`
		// alone hid the fact the next click might dump 50 KB.
		out += r.styles.Dim.Render(fmt.Sprintf(" … +%d more · %s", hidden, humanBytes(totalBytes)))
	case totalBytes >= 1024:
		// fully shown but heavy — surface size so user knows the LLM is
		// paying for a chunky payload even if scrollback hides it.
		out += r.styles.Dim.Render(fmt.Sprintf(" · %s", humanBytes(totalBytes)))
	}
	return out + "\n"
}

// RenderThinkingPartial is the live-streaming variant of renderThinking
// used by the in-flight view region. Same dim/italic styling so the live
// block visually matches the finalized scrollback render once the turn
// commits. Empty result when thoughts are hidden or no content remains
// after sanitize.
func (r *StreamRenderer) RenderThinkingPartial(s string) string {
	body := r.renderThinking(s)
	if body == "" || r.compact {
		return body
	}
	return applyGutter(body)
}

// renderThinking styles a chain-of-thought block: every line dimmed +
// italic and prefixed with a quiet `·` glyph so it visually recedes behind
// the actual answer. ANSI strip + control sanitize follow the same rules
// as tool output so escapes can't blit over chrome.
func (r *StreamRenderer) renderThinking(s string) string {
	if !r.showThoughts {
		return ""
	}
	clean := ansi.Strip(strings.TrimRight(s, "\n"))
	if clean == "" {
		return ""
	}
	lines := strings.Split(clean, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		ln = sanitizeControl(ln)
		if strings.TrimSpace(ln) == "" {
			continue
		}
		out = append(out, r.styles.Dim.Render("·")+" "+r.styles.Thought.Render(ln))
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

// compactLines splits clean output and drops blank/whitespace-only lines so
// previews don't waste rows on padding. Order is preserved. Non-printable
// control bytes (NUL, BS, BEL, raw CR — anything below 0x20 except tab) are
// replaced with `·` per line: grep accidentally hitting a binary blob,
// curl spitting raw bytes, etc. would otherwise blit blank rows + scrambled
// glyphs into the terminal even after ansi.Strip.
func compactLines(s string) []string {
	if s == "" {
		return nil
	}
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		l = sanitizeControl(l)
		if strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, strings.TrimRight(l, " \t"))
	}
	return out
}

// sanitizeControl drops every byte < 0x20 (except tab) and 0x7f. ansi.Strip
// handles CSI/OSC sequences; this catches the bare control bytes (NUL, CR,
// BS, BEL, ...) that wreck terminal layout when raw bytes leak in. Dropping
// (not substituting) lets a line of pure NULs collapse to empty so compactLines
// then discards it — no rows wasted on what was binary garbage.
func sanitizeControl(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\t' || (r >= 0x20 && r != 0x7f) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
