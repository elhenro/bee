package hashline_edit

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/elhenro/bee/internal/tools/apply_patch"
)

func parseEdits(raw []any) ([]edit, error) {
	out := make([]edit, 0, len(raw))
	for i, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("edit %d: not an object", i)
		}
		pos, _ := m["pos"].(string)
		op, _ := m["op"].(string)
		linesAny, _ := m["lines"].([]any)

		ln, tag, err := parsePos(pos)
		if err != nil {
			return nil, err
		}
		if op != "replace" && op != "prepend" && op != "append" {
			return nil, fmt.Errorf("bad op %q", op)
		}
		// reject empty replace early: dropping a line with no replacement
		// is silent data loss and the spec defines no delete op.
		if op == "replace" && len(linesAny) == 0 {
			return nil, fmt.Errorf("edit %d: replace requires at least one line", i)
		}
		lines := make([]string, 0, len(linesAny))
		for j, l := range linesAny {
			s, ok := l.(string)
			if !ok {
				return nil, fmt.Errorf("edit %d line %d: not a string", i, j)
			}
			lines = append(lines, s)
		}
		out = append(out, edit{rawPos: pos, line: ln, tag: tag, op: op, lines: lines})
	}
	return out, nil
}

func parsePos(pos string) (int, string, error) {
	hash := strings.IndexByte(pos, '#')
	if hash <= 0 || hash != len(pos)-4 {
		return 0, "", fmt.Errorf("bad pos %q; want <lineNumber>#<3-char-tag>", pos)
	}
	ln, err := strconv.Atoi(pos[:hash])
	if err != nil || ln < 1 {
		return 0, "", fmt.Errorf("bad pos %q; want <lineNumber>#<3-char-tag>", pos)
	}
	tag := pos[hash+1:]
	if len(tag) != 3 {
		return 0, "", fmt.Errorf("bad pos %q; want <lineNumber>#<3-char-tag>", pos)
	}
	return ln, tag, nil
}

func validateEdits(edits []edit, lines []string) error {
	seen := make(map[int]bool, len(edits))
	for _, e := range edits {
		if seen[e.line] {
			return fmt.Errorf("overlapping edit at pos %s", e.rawPos)
		}
		seen[e.line] = true
		if e.line > len(lines) {
			return fmt.Errorf("line %d out of range; file has %d lines", e.line, len(lines))
		}
		live := lines[e.line-1]
		got := apply_patch.Tag(live, e.line)
		if got != e.tag {
			return fmt.Errorf(
				"hash mismatch at line %d: claimed %s, live %s\n  - expected: %s\n  + actual:   %s",
				e.line, e.tag, got, e.tag, got,
			)
		}
	}
	return nil
}

// applyOne mutates lines per edit. Caller must apply bottom-up so indices
// stay valid across the batch.
func applyOne(lines []string, e edit) []string {
	idx := e.line - 1
	switch e.op {
	case "replace":
		// splice: drop the target line, insert replacement lines in its place.
		head := append([]string{}, lines[:idx]...)
		tail := append([]string{}, lines[idx+1:]...)
		return append(append(head, e.lines...), tail...)
	case "prepend":
		head := append([]string{}, lines[:idx]...)
		tail := append([]string{}, lines[idx:]...)
		return append(append(head, e.lines...), tail...)
	case "append":
		head := append([]string{}, lines[:idx+1]...)
		tail := append([]string{}, lines[idx+1:]...)
		return append(append(head, e.lines...), tail...)
	}
	return lines
}
