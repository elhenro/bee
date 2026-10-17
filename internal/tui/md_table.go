package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// glamour stretches every table to the full wrap width, so three short columns
// spread across the terminal. a table is rendered on its own with a wrap width
// equal to its natural width instead, and prose around it as before.

// newMarkdown builds a glamour renderer at the given wrap width. no
// WithAutoStyle: its OSC 11 reply leaks into the input; the style follows the
// lipgloss flag cmd/bee/tui.go set from BEE_THEME / COLORFGBG.
// WithPreservedNewLines keeps the model's intentional line breaks (lists, short
// prose lines) instead of collapsing them into one paragraph.
func newMarkdown(wrap int) *glamour.TermRenderer {
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(markdownStyle(lipgloss.HasDarkBackground())),
		glamour.WithWordWrap(wrap),
		glamour.WithPreservedNewLines(),
		glamour.WithEmoji(),
	)
	if err != nil {
		return nil
	}
	return r
}

type mdSegment struct {
	text  string
	table bool
}

// splits markdown into prose and pipe tables, leaving fenced code alone
func splitTables(s string) []mdSegment {
	lines := strings.Split(s, "\n")
	var segs []mdSegment
	start, fence := 0, false
	for i := 0; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
			fence = !fence
		}
		if fence || !tableAt(lines, i) {
			continue
		}
		end := i + 2
		for end < len(lines) && strings.Contains(lines[end], "|") && strings.TrimSpace(lines[end]) != "" {
			end++
		}
		if i > start {
			segs = append(segs, mdSegment{text: strings.Join(lines[start:i], "\n")})
		}
		segs = append(segs, mdSegment{text: strings.Join(lines[i:end], "\n"), table: true})
		start, i = end, end-1
	}
	if start < len(lines) {
		segs = append(segs, mdSegment{text: strings.Join(lines[start:], "\n")})
	}
	return segs
}

var delimRow = regexp.MustCompile(`^\s{0,3}\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$`)

// a header row followed by a delimiter row starts a table
func tableAt(lines []string, i int) bool {
	if i+1 >= len(lines) || !strings.Contains(lines[i], "|") || !strings.Contains(lines[i+1], "|") {
		return false
	}
	head := lines[i]
	if strings.TrimSpace(head) == "" || len(head)-len(strings.TrimLeft(head, " ")) > 3 {
		return false
	}
	return delimRow.MatchString(lines[i+1])
}

var mdLink = regexp.MustCompile(`\[([^\]]*)\]\(([^)]*)\)`)

// cell text as it will read once styled: markers gone, a link followed by its url
func plainCell(c string) string {
	c = mdLink.ReplaceAllString(c, "$1 $2")
	for _, m := range []string{"`", "**", "~~"} {
		c = strings.ReplaceAll(c, m, "")
	}
	return strings.TrimSpace(strings.ReplaceAll(c, `\|`, "|"))
}

// cells of one pipe row; a `|` inside a code span or behind a backslash is text
func splitRow(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimPrefix(row, "|")
	var cells []string
	var b strings.Builder
	code := false
	for i := 0; i < len(row); i++ {
		switch c := row[i]; {
		case c == '`':
			code = !code
			b.WriteByte(c)
		case c == '\\' && i+1 < len(row) && row[i+1] == '|':
			b.WriteString(`\|`)
			i++
		case c == '|' && !code:
			cells = append(cells, b.String())
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	if tail := b.String(); strings.TrimSpace(tail) != "" {
		cells = append(cells, tail)
	}
	return cells
}

// width a table needs to show every cell on one row: widest cell per column,
// a margin either side of each, a rule between, and a little slack so a cell
// measured a cell short can't wrap
func tableWidth(tbl string) int {
	var cols []int
	for i, row := range strings.Split(tbl, "\n") {
		if i == 1 {
			continue // delimiter row
		}
		for c, cell := range splitRow(row) {
			w := ansi.StringWidth(plainCell(cell))
			if c == len(cols) {
				cols = append(cols, 0)
			}
			cols[c] = max(cols[c], w)
		}
	}
	total := len(cols) + 1
	for _, w := range cols {
		total += w + 2
	}
	return total
}

// renders markdown, fitting each table to its content
func (r *StreamRenderer) renderMarkdown(s string) (string, error) {
	segs := splitTables(s)
	if len(segs) == 1 && !segs[0].table {
		return r.md.Render(s)
	}
	parts := make([]string, 0, len(segs))
	for _, sg := range segs {
		md := r.md
		if sg.table {
			if w := tableWidth(sg.text); w < r.mdWrap {
				md = r.tableRenderer(w)
			}
		}
		out, err := md.Render(sg.text)
		if err != nil {
			return "", err
		}
		if out = trimBlankLines(out); out != "" {
			parts = append(parts, out)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// one renderer per table width, built on first use
func (r *StreamRenderer) tableRenderer(w int) *glamour.TermRenderer {
	if md, ok := r.mdTables[w]; ok {
		return md
	}
	md := newMarkdown(w)
	if md == nil {
		md = r.md
	}
	if r.mdTables == nil {
		r.mdTables = make(map[int]*glamour.TermRenderer)
	}
	r.mdTables[w] = md
	return md
}
