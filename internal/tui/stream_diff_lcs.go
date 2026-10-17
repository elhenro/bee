package tui

import "fmt"

// editOp is one step in a line-level diff: kind ∈ {'=','+','-','~'} where
// '~' marks a synthetic gap line ("⋯ +K unchanged") emitted by the
// collapser. text is the line content (or marker payload for '~'). old/new
// are the file line numbers each side stood at before this step (0 = not
// numbered); see numberOps.
type editOp struct {
	kind     byte
	text     string
	old, new int
}

// diffMaxCells caps the LCS DP grid so a runaway edit (e.g. write of a huge
// generated file) can't lock the renderer. 250k cells ≈ 500x500 lines.
const diffMaxCells = 250000

// lineDiff computes an LCS-based edit script between two slices of lines.
// Greedy DP — runtime is O(n·m) which is fine for the <few-hundred-line
// payloads typical of an edit tool call. For pathologically large inputs
// (n·m > diffMaxCells), falls back to a naive "all dels then all adds" script
// so the renderer stays responsive instead of stalling the TUI thread.
func lineDiff(a, b []string) []editOp {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil
	}
	if n*m > diffMaxCells {
		out := make([]editOp, 0, n+m)
		for _, l := range a {
			out = append(out, editOp{kind: '-', text: l})
		}
		for _, l := range b {
			out = append(out, editOp{kind: '+', text: l})
		}
		return out
	}
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
				continue
			}
			if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	out := make([]editOp, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, editOp{kind: '=', text: a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, editOp{kind: '-', text: a[i]})
			i++
		default:
			out = append(out, editOp{kind: '+', text: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, editOp{kind: '-', text: a[i]})
	}
	for ; j < m; j++ {
		out = append(out, editOp{kind: '+', text: b[j]})
	}
	return out
}

// numberOps stamps ops with line numbers counted up from start, the file line
// the first step sits on (the old and new side begin on the same line because
// everything above an edit is untouched)
func numberOps(ops []editOp, start int) {
	o, n := start, start
	for i := range ops {
		ops[i].old, ops[i].new = o, n
		switch ops[i].kind {
		case '=':
			o++
			n++
		case '-':
			o++
		case '+':
			n++
		}
	}
}

// countDiffOps tallies real adds/dels (ignoring '=' context and '~' markers)
// for the header `+N −M` badge.
func countDiffOps(ops []editOp) (adds, dels int) {
	for _, op := range ops {
		switch op.kind {
		case '+':
			adds++
		case '-':
			dels++
		}
	}
	return
}

// collapseToHunks trims unchanged ('=') runs to at most `ctx` lines on each
// side of a change block, replacing the middle of long runs with a '~' gap
// marker. Mirrors `git diff -U<ctx>` behaviour but cheaper since we just
// rewrite the op stream.
func collapseToHunks(ops []editOp, ctx int) []editOp {
	if len(ops) == 0 {
		return ops
	}
	type span struct{ lo, hi int }
	var changes []span
	i := 0
	for i < len(ops) {
		if ops[i].kind == '=' {
			i++
			continue
		}
		j := i
		for j < len(ops) && ops[j].kind != '=' {
			j++
		}
		changes = append(changes, span{i, j})
		i = j
	}
	if len(changes) == 0 {
		return nil
	}
	out := make([]editOp, 0, len(ops))
	prevEnd := 0
	for idx, c := range changes {
		gap := ops[prevEnd:c.lo]
		switch {
		case idx == 0 && len(gap) > ctx:
			hidden := len(gap) - ctx
			out = append(out, editOp{kind: '~', text: fmt.Sprintf("+%d unchanged above", hidden)})
			gap = gap[len(gap)-ctx:]
		case idx > 0 && len(gap) > 2*ctx:
			hidden := len(gap) - 2*ctx
			out = append(out, gap[:ctx]...)
			out = append(out, editOp{kind: '~', text: fmt.Sprintf("+%d unchanged", hidden)})
			gap = gap[len(gap)-ctx:]
		}
		out = append(out, gap...)
		out = append(out, ops[c.lo:c.hi]...)
		prevEnd = c.hi
	}
	tail := ops[prevEnd:]
	if len(tail) > ctx {
		hidden := len(tail) - ctx
		out = append(out, tail[:ctx]...)
		out = append(out, editOp{kind: '~', text: fmt.Sprintf("+%d unchanged below", hidden)})
	} else {
		out = append(out, tail...)
	}
	return out
}
