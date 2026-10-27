package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/elhenro/bee/internal/types"
)

// memo for renders that repeat on every frame of a streaming turn
type liveCache struct {
	// header of in-flight file-mutation calls by use id; the diff behind it
	// is built once, not every frame
	editHeads map[string]string
	// reasoning rows of the lines that can no longer change
	thoughtSrc   string
	thoughtWidth int
	thoughtRows  []string
}

// a turn's batch is small; past this the memo starts over
const maxEditHeads = 64

// the header of a pending file-mutation call. ok is false for other tools
func (r *StreamRenderer) pendingEditHead(u types.ToolUse) (head string, ok bool) {
	if !isEditTool(u.Name) {
		return "", false
	}
	if h, hit := r.live.editHeads[u.ID]; hit {
		return h, true
	}
	head, _, _ = r.renderEditPreview(u, 0)
	if u.ID != "" {
		if r.live.editHeads == nil || len(r.live.editHeads) >= maxEditHeads {
			r.live.editHeads = map[string]string{}
		}
		r.live.editHeads[u.ID] = head
	}
	return head, true
}

// wrapped, styled reasoning rows; nil when there is no visible text. lines up
// to the last newline are final, so their rows are kept between calls and only
// the line being typed is wrapped and styled again
func (r *StreamRenderer) thoughtRows(s string) []string {
	cut := strings.LastIndexByte(s, '\n') + 1
	// an escape sequence may span lines, so strip such text in one piece
	if strings.IndexByte(s, 0x1b) >= 0 {
		cut = 0
	}
	settled, tail := s[:cut], s[cut:]
	c, w := &r.live, r.textWidth(markIndent)
	if c.thoughtWidth != w || !strings.HasPrefix(settled, c.thoughtSrc) {
		c.thoughtSrc, c.thoughtRows, c.thoughtWidth = "", nil, w
	}
	if len(settled) > len(c.thoughtSrc) {
		c.thoughtRows = append(c.thoughtRows, r.styleThought(settled[len(c.thoughtSrc):], w)...)
		c.thoughtSrc = settled
	}
	rows := slices.Concat(c.thoughtRows, r.styleThought(tail, w))
	if len(rows) == 0 {
		return nil
	}
	return rows
}

// styled rows of reasoning text, stripped like tool output so escapes can't
// blit over the chrome
func (r *StreamRenderer) styleThought(s string, w int) []string {
	clean := expandTabs(ansi.Strip(strings.TrimRight(s, "\n")))
	var rows []string
	for ln := range strings.SplitSeq(clean, "\n") {
		ln = sanitizeControl(ln)
		if strings.TrimSpace(ln) == "" {
			continue
		}
		rows = append(rows, strings.Split(wrapText(ln, w), "\n")...)
	}
	// style per line: SGR must not bleed across the hanging indent
	for i := range rows {
		rows[i] = r.styles.Thought.Render(rows[i])
	}
	return rows
}
