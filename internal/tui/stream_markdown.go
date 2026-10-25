package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (r *StreamRenderer) renderText(s string) string {
	return r.renderTextAs(s, false)
}

// ui marks app-generated text, whose aligned rows wrap with a hanging indent
func (r *StreamRenderer) renderTextAs(s string, ui bool) string {
	if s == "" {
		return s
	}
	s = expandTabs(s) // before wrapping, so width math sees the real cells
	plain := func() string {
		if ui {
			return wrapColumns(s, r.textWidth(markIndent))
		}
		return wrapText(s, r.textWidth(markIndent))
	}
	if r.md == nil {
		return plain()
	}
	// short-circuit: glamour's document margin and surrounding blank lines
	// turn "Yo." into "  Yo." sandwiched in blanks. Plain text gets returned
	// raw; only invoke glamour when there's actual markdown to style.
	if !needsMarkdown(s) {
		return plain()
	}
	s = rewriteShellSessionFences(s)
	out, err := r.renderMarkdown(s)
	if err != nil {
		return s
	}
	return clampRows(trimBlankLines(dedent(out)), r.textWidth(markIndent))
}

// trimBlankLines drops leading and trailing rows that are blank once styling
// is stripped. glamour pads blocks (code fences, quotes) with rows of bare
// spaces and SGR resets that a plain Trim("\n") misses; left in, they stack
// on the paragraph gap the message frame already adds.
func trimBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(ansi.Strip(lines[start])) == "" {
		start++
	}
	for end > start && strings.TrimSpace(ansi.Strip(lines[end-1])) == "" {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}

// needsMarkdown is a crude marker check — fences, inline code, leading list/
// heading/quote chars, numbered lists, horizontal rules, links, or bold/em
// emphasis. Catches prose that benefits from glamour; lets bare chat replies
// through untouched so a "yo." doesn't get framed in document margins.
func needsMarkdown(s string) bool {
	// "](" is an inline link: a cheap substring check covers the well-formed
	// case; false positives on prose are harmless.
	for _, marker := range []string{"`", "**", "__", "]("} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	for _, ln := range strings.Split(s, "\n") {
		t := strings.TrimLeft(ln, " \t")
		if t == "" {
			continue
		}
		if strings.IndexByte("#>-*|", t[0]) >= 0 || numberedItem.MatchString(t) {
			return true
		}
		// horizontal rule of underscores; --- and *** matched above
		if t[0] == '_' && len(t) >= 3 && strings.Trim(t, "_ ") == "" {
			return true
		}
	}
	return false
}

// numbered list item: `1. `, `12) `
var numberedItem = regexp.MustCompile(`^[0-9]+[.)] `)

// rewriteShellSessionFences swaps ```bash/```sh/```shell fence tags to
// ```console when the block's first non-empty line starts with a `$ ` or
// `> ` prompt. chroma's `bash` lexer treats every line as bash, so prompt
// output (git log subjects, etc) gets keyword-colored ("log", "for", "in",
// "hash" lit up as bash builtins). The `bash_session`/`console` lexer
// scopes bash coloring to the prompt line only and leaves output plain.
func rewriteShellSessionFences(s string) string {
	if !strings.Contains(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	fenceIdx := -1 // index of open bash-tagged fence line awaiting probe; -1 = none
	for i, ln := range lines {
		t := strings.TrimRight(ln, " \t")
		if fenceIdx < 0 {
			if !strings.HasPrefix(t, "```") {
				continue
			}
			tag := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(t, "```")))
			switch tag {
			case "bash", "sh", "shell", "zsh":
				fenceIdx = i
			}
			continue
		}
		// inside a probe-pending fence
		if strings.HasPrefix(t, "```") {
			fenceIdx = -1 // empty body or closed before probe
			continue
		}
		if strings.TrimSpace(ln) == "" {
			continue
		}
		body := strings.TrimLeft(ln, " \t")
		if strings.HasPrefix(body, "$ ") || strings.HasPrefix(body, "> ") || body == "$" {
			lines[fenceIdx] = "```console"
		}
		fenceIdx = -1 // probe done; ignore rest of block
	}
	return strings.Join(lines, "\n")
}

// dedent strips the common leading-space prefix from every non-blank line and
// trims trailing spaces. Glamour's standard style adds a document margin (we
// don't want it eating columns) and right-pads lines for table/quote bg
// fill (which paints color bands at the right edge of the terminal). Both
// look like junk in our compact layout.
func dedent(s string) string {
	lines := strings.Split(s, "\n")
	// measure the indent on *visible* text: ANSI escapes glamour inserts at
	// the start of a line (e.g. color reset) must not count toward it, yet
	// stay in the output, so strip in a working copy just for the measurement
	indent := -1
	for _, l := range lines {
		visible := ansi.Strip(l)
		if strings.TrimSpace(visible) == "" {
			continue
		}
		n := len(visible) - len(strings.TrimLeft(visible, " "))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	for i, l := range lines {
		if indent > 0 {
			l = stripLeadingSpacesPreservingANSI(l, indent)
		}
		lines[i] = trimTrailingVisibleSpaces(l)
	}
	return strings.Join(lines, "\n")
}

// csiSeqEnd is the end of the CSI escape sequence starting at s[i] (up to its
// terminator byte in 0x40-0x7e), or i when none starts there.
func csiSeqEnd(s string, i int) int {
	if s[i] != 0x1b || i+1 >= len(s) || s[i+1] != '[' {
		return i
	}
	j := i + 2
	for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
		j++
	}
	return min(j+1, len(s))
}

// trimTrailingVisibleSpaces drops trailing whitespace from l even when the
// whitespace is wrapped in per-span ANSI color spans. Glamour right-pads
// every line to the wrap width and emits `\x1b[38;5;252m \x1b[0m` per space,
// so plain TrimRight sees `m` at the end and does nothing.
//
// Splits l into escape sequences and the visible runs between them, and cuts
// after the last run holding a non-blank byte; that run is kept whole.
func trimTrailingVisibleSpaces(l string) string {
	keep := 0
	for i := 0; i < len(l); {
		if j := csiSeqEnd(l, i); j > i {
			i = j
			continue
		}
		j := i + 1
		for j < len(l) && csiSeqEnd(l, j) == j {
			j++
		}
		if strings.Trim(l[i:j], " \t") != "" {
			keep = j
		}
		i = j
	}
	return l[:keep]
}

// stripLeadingSpacesPreservingANSI removes up to n visible leading spaces
// from l while leaving ANSI escape sequences intact. Glamour emits a color
// reset before the document margin, so a naive l[n:] would slice mid-escape
// and corrupt the rest of the line.
func stripLeadingSpacesPreservingANSI(l string, n int) string {
	var b strings.Builder
	b.Grow(len(l))
	removed := 0
	for i := 0; i < len(l); {
		if j := csiSeqEnd(l, i); j > i {
			b.WriteString(l[i:j])
			i = j
			continue
		}
		if l[i] != ' ' || removed >= n {
			b.WriteString(l[i:])
			break
		}
		removed++
		i++
	}
	return b.String()
}
