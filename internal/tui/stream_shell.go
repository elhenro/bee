package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/elhenro/bee/internal/types"
)

// inlineShellPrefix marks a message body as an inline `!cmd` / `!!cmd`
// shell-exec record. Stored as the first line so the renderer can route to
// renderInlineShell. Plain text so the LLM (which sees this in context) can
// still understand it as a shell invocation block.
const inlineShellPrefix = "[shell exit="

// formatInlineShell builds the stored text for an inline shell record:
//
//	[shell exit=N]
//	$ <cmd>
//	<output>
//
// The first-line marker is the parser hook for renderInlineShell; the rest
// reads naturally so the LLM (which sees this in context for !cmd) can
// understand it as a shell exec block.
func formatInlineShell(cmd, output string, isErr bool) string {
	exit := 0
	if isErr {
		exit = 1
	}
	out := strings.TrimRight(output, "\n")
	if out == "" {
		return fmt.Sprintf("%s%d]\n$ %s", inlineShellPrefix, exit, cmd)
	}
	return fmt.Sprintf("%s%d]\n$ %s\n%s", inlineShellPrefix, exit, cmd, out)
}

// parseInlineShell extracts cmd, output, and exit-status from a message body
// produced by formatInlineShell. Returns ok=false if the text doesn't match.
func parseInlineShell(text string) (cmd, output string, isErr, ok bool) {
	if !strings.HasPrefix(text, inlineShellPrefix) {
		return "", "", false, false
	}
	rest := text[len(inlineShellPrefix):]
	closeIdx := strings.Index(rest, "]\n")
	if closeIdx < 0 {
		return "", "", false, false
	}
	exitStr := rest[:closeIdx]
	body := rest[closeIdx+2:]
	// expect first body line to be "$ <cmd>"
	nl := strings.IndexByte(body, '\n')
	var cmdLine string
	if nl < 0 {
		cmdLine = body
		output = ""
	} else {
		cmdLine = body[:nl]
		output = body[nl+1:]
	}
	if !strings.HasPrefix(cmdLine, "$ ") {
		return "", "", false, false
	}
	cmd = cmdLine[2:]
	isErr = exitStr != "0"
	return cmd, output, isErr, true
}

// renderInlineShell styles an inline shell record:
//
//	$ ls                  honey prompt + command
//	  ⎿  file1.go         output hangs off it, ANSI-stripped
//	     file2.go
//
// Output spans every row (no preview clip like tool cards): you asked for it.
// The honey prompt reads at a glance as "this came from !cmd / !!cmd" rather
// than blending into regular prose.
func (r *StreamRenderer) renderInlineShell(cmd, output string, isErr bool) string {
	honey := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
	header := honey.Render("$ " + cmd) // single styled span keeps coloring contiguous
	clean := expandTabs(ansi.Strip(strings.TrimRight(output, "\n")))
	if clean == "" {
		return header
	}
	bodyStyle := r.styles.ToolPrev
	if isErr {
		bodyStyle = r.styles.Error
	}
	w := r.textWidth(resultIndent)
	lines := strings.Split(clean, "\n")
	for i, ln := range lines {
		lines[i] = bodyStyle.Render(clipLine(sanitizeControl(ln), w))
	}
	return header + "\n" + hang(strings.Join(lines, "\n"), r.connector(), strings.Repeat(" ", resultIndent))
}

// isNudgeMessage reports whether m is a synthetic recovery nudge emitted by
// the loop (reasoning-only stall → injected user turn whose text starts with
// the `[nudge]` marker). Used to suppress them from scrollback unless the
// `show_nudges` setting flips it on.
func isNudgeMessage(m types.Message) bool {
	if m.Role != types.RoleUser {
		return false
	}
	tb := firstTextBlock(m)
	if tb == nil {
		return false
	}
	return strings.HasPrefix(strings.TrimLeft(tb.Text, " \t"), "[nudge]")
}

// firstTextBlock returns the first BlockText in m (nil if none). Used to keep
// inline-shell detection working when an image companion is staged.
func firstTextBlock(m types.Message) *types.ContentBlock {
	for i := range m.Content {
		if m.Content[i].Type == types.BlockText {
			return &m.Content[i]
		}
	}
	return nil
}
