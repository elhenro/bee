package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (r *StreamRenderer) renderText(s string) string {
	if r.md == nil || s == "" {
		return s
	}
	// short-circuit: glamour's document margin and surrounding blank lines
	// turn "Yo." into "  Yo." sandwiched in blanks. Plain text gets returned
	// raw; only invoke glamour when there's actual markdown to style.
	if !needsMarkdown(s) {
		return s
	}
	s = rewriteShellSessionFences(s)
	out, err := r.md.Render(s)
	if err != nil {
		return s
	}
	return dedent(strings.Trim(out, "\n"))
}

// needsMarkdown is a crude marker check — fences, inline code, leading list/
// heading/quote chars, numbered lists, horizontal rules, links, or bold/em
// emphasis. Catches prose that benefits from glamour; lets bare chat replies
// through untouched so a "yo." doesn't get framed in document margins.
func needsMarkdown(s string) bool {
	if strings.Contains(s, "```") || strings.Contains(s, "`") {
		return true
	}
	if strings.Contains(s, "**") || strings.Contains(s, "__") {
		return true
	}
	// inline link: [text](url) — cheap substring check covers the
	// well-formed case; false positives on prose are harmless.
	if strings.Contains(s, "](") {
		return true
	}
	for _, ln := range strings.Split(s, "\n") {
		t := strings.TrimLeft(ln, " \t")
		if t == "" {
			continue
		}
		switch t[0] {
		case '#', '>', '-', '*', '|':
			return true
		}
		// numbered list: `1. `, `12. ` — digits then `. ` or `) `.
		if t[0] >= '0' && t[0] <= '9' {
			i := 0
			for i < len(t) && t[i] >= '0' && t[i] <= '9' {
				i++
			}
			if i < len(t)-1 && (t[i] == '.' || t[i] == ')') && t[i+1] == ' ' {
				return true
			}
		}
		// horizontal rule: --- *** ___
		if len(t) >= 3 {
			c := t[0]
			if c == '-' || c == '*' || c == '_' {
				all := true
				for j := 0; j < len(t); j++ {
					if t[j] != c && t[j] != ' ' {
						all = false
						break
					}
				}
				if all {
					return true
				}
			}
		}
	}
	return false
}

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
	// dedent leading spaces uses *visible* leading width; ANSI escapes
	// inserted by glamour at the start of a line (e.g. color reset) must
	// not count toward the indent. ansi.Strip via the surrounding pipeline
	// is too aggressive — we still want the escapes preserved. So we strip
	// in a working copy just for the measurement.
	min := -1
	for _, l := range lines {
		visible := ansi.Strip(l)
		if strings.TrimSpace(visible) == "" {
			continue
		}
		n := len(visible) - len(strings.TrimLeft(visible, " "))
		if min < 0 || n < min {
			min = n
		}
	}
	if min < 0 {
		min = 0
	}
	for i, l := range lines {
		if min > 0 {
			l = stripLeadingSpacesPreservingANSI(l, min)
		}
		lines[i] = trimTrailingVisibleSpaces(l)
	}
	return strings.Join(lines, "\n")
}

// trimTrailingVisibleSpaces drops trailing whitespace from l even when the
// whitespace is wrapped in per-span ANSI color spans. Glamour right-pads
// every line to the wrap width and emits `\x1b[38;5;252m \x1b[0m` per space,
// so plain TrimRight sees `m` at the end and does nothing.
//
// Algorithm: split l into [span | visible] runs by scanning ANSI escapes.
// Drop trailing runs whose visible content is empty or whitespace-only,
// keeping the preceding ANSI escapes that bracket non-whitespace content.
func trimTrailingVisibleSpaces(l string) string {
	if l == "" {
		return l
	}
	// find offsets of each ANSI escape and the visible bytes between them.
	type seg struct {
		isAnsi bool
		s, e   int // [s, e)
	}
	segs := make([]seg, 0, 16)
	i := 0
	for i < len(l) {
		if l[i] == 0x1b && i+1 < len(l) && l[i+1] == '[' {
			j := i + 2
			for j < len(l) && (l[j] < 0x40 || l[j] > 0x7e) {
				j++
			}
			if j < len(l) {
				j++
			}
			segs = append(segs, seg{isAnsi: true, s: i, e: j})
			i = j
			continue
		}
		// visible run until next ESC
		j := i
		for j < len(l) && l[j] != 0x1b {
			j++
		}
		segs = append(segs, seg{isAnsi: false, s: i, e: j})
		i = j
	}
	// walk from the end dropping visible runs that are blank-only; ansi
	// runs between blanks are dropped too. Stop once we hit a non-blank
	// visible run.
	cut := len(l)
	for k := len(segs) - 1; k >= 0; k-- {
		sg := segs[k]
		if sg.isAnsi {
			cut = sg.s
			continue
		}
		blank := true
		for p := sg.s; p < sg.e; p++ {
			if l[p] != ' ' && l[p] != '\t' {
				blank = false
				break
			}
		}
		if blank {
			cut = sg.s
			continue
		}
		// keep this visible run intact
		break
	}
	return l[:cut]
}

// stripLeadingSpacesPreservingANSI removes up to n visible leading spaces
// from l while leaving ANSI escape sequences intact. Glamour emits a color
// reset before the document margin, so a naive l[n:] would slice mid-escape
// and corrupt the rest of the line.
func stripLeadingSpacesPreservingANSI(l string, n int) string {
	if n <= 0 || l == "" {
		return l
	}
	var b strings.Builder
	b.Grow(len(l))
	removed := 0
	i := 0
	for i < len(l) {
		if l[i] == 0x1b && i+1 < len(l) && l[i+1] == '[' {
			// copy through to terminator byte (alpha in 0x40-0x7e)
			j := i + 2
			for j < len(l) && (l[j] < 0x40 || l[j] > 0x7e) {
				j++
			}
			if j < len(l) {
				j++
			}
			b.WriteString(l[i:j])
			i = j
			continue
		}
		if l[i] == ' ' && removed < n {
			removed++
			i++
			continue
		}
		b.WriteString(l[i:])
		break
	}
	return b.String()
}
