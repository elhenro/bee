package tui

import (
	"fmt"
	"strconv"
	"strings"
)

// one row of a diff card before styling
type diffRow struct {
	kind byte // '+' added, '-' removed, '=' context, 'm' meta
	no   int  // gutter line number, 0 = none
	text string
}

// a file's rows under an optional pre-styled label; path picks the lexer
type diffBlock struct {
	label string
	path  string
	rows  []diffRow
}

// line number a step shows: removals count on the old side, the rest on the new
func (o editOp) lineNo() int {
	if o.kind == '-' {
		return o.old
	}
	return o.new
}

// old-side lines a step eats
func (o editOp) eats() int {
	if o.kind == '=' || o.kind == '-' {
		return 1
	}
	return 0
}

// collapsed ops to rows. numbered ops show their line numbers and the gap
// between two shown rows reads off the numbers (exact even across patch
// hunks); unnumbered ops fall back to the collapser's own markers
func layoutRows(ops []editOp) []diffRow {
	// splitKeepEmpty keeps the blank after a trailing newline, nothing to show
	for len(ops) > 0 && ops[len(ops)-1].kind == '=' && ops[len(ops)-1].text == "" {
		ops = ops[:len(ops)-1]
	}
	numbered := false
	for _, op := range ops {
		if op.old > 0 || op.new > 0 {
			numbered = true
			break
		}
	}
	rows := make([]diffRow, 0, len(ops)+2)
	var prev *editOp
	for i := range ops {
		op := &ops[i]
		if op.kind == '~' {
			if !numbered {
				rows = append(rows, diffRow{kind: 'm', text: glyphGap + " " + op.text})
			}
			continue
		}
		if numbered && prev != nil {
			if k := op.old - prev.old - prev.eats(); k > 0 {
				rows = append(rows, diffRow{kind: 'm', text: fmt.Sprintf("%s +%d unchanged", glyphGap, k)})
			}
		}
		rows = append(rows, diffRow{kind: op.kind, no: op.lineNo(), text: op.text})
		prev = op
	}
	return rows
}

// caps rows at keep with a `… +N lines` marker where the cut falls. a long
// run of removals up front would push every addition off the card, so then
// the budget splits between head and tail
func trimRows(rows []diffRow, keep int) []diffRow {
	if keep <= 0 || len(rows) <= keep {
		return rows
	}
	lead := 0
	for lead < len(rows) && rows[lead].kind == '-' {
		lead++
	}
	head, tail := keep, 0
	if lead > keep/2 {
		head = keep / 2
		tail = keep - head
	}
	out := make([]diffRow, 0, keep+1)
	out = append(out, rows[:head]...)
	out = append(out, diffRow{kind: 'm', text: fmt.Sprintf("%s +%d lines", glyphMore, len(rows)-head-tail)})
	return append(out, rows[len(rows)-tail:]...)
}

// digits of the widest line number, 0 when nothing is numbered
func gutterWidth(blocks []diffBlock) int {
	top := 0
	for _, b := range blocks {
		for _, rw := range b.rows {
			top = max(top, rw.no)
		}
	}
	if top == 0 {
		return 0
	}
	return len(strconv.Itoa(top))
}

// one styled row: `<no> <sign> <code>`, changes wash the full width
func (r *StreamRenderer) styleRow(rw diffRow, lang string, gw, width int) string {
	under := ""
	if gw > 0 {
		under = strings.Repeat(" ", gw+1)
	}
	num := under
	if gw > 0 && rw.no > 0 {
		num = fmt.Sprintf("%*d ", gw, rw.no)
	}
	switch rw.kind {
	case '+', '-':
		sign, sty, bg := "+", r.styles.DiffAdd, r.styles.DiffAddBg
		if rw.kind == '-' {
			sign, sty, bg = "-", r.styles.DiffDel, r.styles.DiffDelBg
		}
		body := expandTabs(rw.text)
		inner := ""
		if num != "" {
			inner = r.styles.Dim.Render(num)
		}
		if r.highlight && lang != "" {
			inner += sty.Render(sign) + " " + HighlightCode(body, lang)
		} else {
			inner += sty.Render(sign + " " + body)
		}
		return bg.Render(preserveBgAcrossResets(padTo(inner, width)))
	case '=':
		return r.styles.Dim.Render(num + "  " + expandTabs(rw.text))
	}
	return r.styles.DiffMeta.Render(under + rw.text)
}

// diff rows hung off the call header. outside verbose each file is capped so
// a big patch can't flood scrollback; the budget splits across files
func (r *StreamRenderer) diffCard(blocks []diffBlock) string {
	keep := diffPreviewLinesCompact
	if n := len(blocks); n > 1 {
		keep = max(keep/n, 4)
	}
	if !r.verbose {
		for i := range blocks {
			r.clipped = r.clipped || len(blocks[i].rows) > keep
			blocks[i].rows = trimRows(blocks[i].rows, keep)
		}
	}
	gw := gutterWidth(blocks)
	w := r.textWidth(resultIndent)
	var out []string
	for _, b := range blocks {
		if b.label != "" {
			out = append(out, clipLine(b.label, w))
		}
		lang := langFromPath(b.path)
		for _, rw := range b.rows {
			out = append(out, clipLine(r.styleRow(rw, lang, gw, w), w))
		}
	}
	if len(out) == 0 {
		return ""
	}
	return hang(strings.Join(out, "\n"), r.connector(), strings.Repeat(" ", resultIndent))
}
