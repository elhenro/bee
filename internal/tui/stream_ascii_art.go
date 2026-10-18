package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// a picture rendered as characters (an image dumped through a shell script)
// previews as its top few rows, which says nothing. Runs of such rows fold to
// one summary line; verbose keeps every row, blank ones included, so the
// shape survives.
const (
	artMinRows   = 6  // art rows a run needs before it counts as a picture
	artMinWidth  = 16 // narrower rows are far likelier prose or a table cell
	artMaxGlyphs = 16 // distinct non-space runes, ramps are short
)

type artRowKind int

const (
	rowOther artRowKind = iota
	rowBlank            // empty after trimming
	rowSoft             // mostly space: a bright patch, only part of a run
	rowArt
)

// folds picture runs in cleaned tool output into summary rows (kept whole
// when verbose); blank rows outside a picture are dropped as everywhere else
func foldArt(clean string, verbose bool) []string {
	if strings.Count(clean, "\n") < artMinRows {
		return compactLines(clean)
	}
	raw := strings.Split(clean, "\n")
	kinds := make([]artRowKind, len(raw))
	for i, l := range raw {
		raw[i] = strings.TrimRight(sanitizeControl(l), " \t")
		kinds[i] = classifyArtRow(raw[i])
	}
	var out []string
	for i := 0; i < len(raw); {
		end := artRunEnd(kinds, i)
		if end == i {
			if kinds[i] != rowBlank {
				out = append(out, raw[i])
			}
			i++
			continue
		}
		out = append(out, artRows(raw[i:end], verbose)...)
		i = end
	}
	return out
}

// exclusive end of the picture run starting at i, i itself when none does:
// art rows joined by blank/soft rows, trimmed to end on an art row
func artRunEnd(kinds []artRowKind, i int) int {
	if kinds[i] != rowArt {
		return i
	}
	end, art := i, 0
	for j := i; j < len(kinds) && kinds[j] != rowOther; j++ {
		if kinds[j] == rowArt {
			art++
			end = j + 1
		}
	}
	if art < artMinRows {
		return i
	}
	return end
}

func artRows(rows []string, verbose bool) []string {
	if verbose {
		return rows
	}
	cols := 0
	for _, l := range rows {
		cols = max(cols, utf8.RuneCountInString(l))
	}
	return []string{fmt.Sprintf("ascii art %s %d rows × %d cols", glyphSep, len(rows), cols)}
}

// art = wide row, dense, few glyphs, and neighbors mostly repeat (a ramp
// paints runs, prose and tables don't)
func classifyArtRow(l string) artRowKind {
	width := utf8.RuneCountInString(l)
	if strings.TrimSpace(l) == "" {
		return rowBlank
	}
	if width < artMinWidth {
		return rowOther
	}
	glyphs := map[rune]struct{}{}
	solid, pairs, same := 0, 0, 0
	var prev rune
	for _, r := range l {
		if r == ' ' || r == '\t' {
			prev = 0
			continue
		}
		solid++
		glyphs[r] = struct{}{}
		if prev != 0 {
			pairs++
			if r == prev {
				same++
			}
		}
		prev = r
	}
	if len(glyphs) > artMaxGlyphs {
		return rowOther
	}
	if solid*10 < width*4 {
		return rowSoft
	}
	if pairs == 0 || same*10 < pairs*6 {
		return rowOther
	}
	return rowArt
}
