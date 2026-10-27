package tui

import (
	"fmt"
	"slices"
)

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
	out := make([]editOp, 0, n+m)
	i, j := 0, 0
	// past the cap only the tail loops below run: all dels, then all adds
	if n*m <= diffMaxCells {
		dp := lcsTable(a, b)
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
	}
	for ; i < n; i++ {
		out = append(out, editOp{kind: '-', text: a[i]})
	}
	for ; j < m; j++ {
		out = append(out, editOp{kind: '+', text: b[j]})
	}
	return out
}

// lcsTable[i][j] is the longest common subsequence length of a[i:] and b[j:]
func lcsTable(a, b []string) [][]int {
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i, v := range slices.Backward(a) {
		for j := len(b) - 1; j >= 0; j-- {
			if v == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	return dp
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
	if !slices.ContainsFunc(ops, func(o editOp) bool { return o.kind != '=' }) {
		return nil
	}
	gap := func(hidden int, where string) editOp {
		return editOp{kind: '~', text: fmt.Sprintf("+%d unchanged%s", hidden, where)}
	}
	var out []editOp
	for i := 0; i < len(ops); {
		j := i
		for j < len(ops) && (ops[j].kind == '=') == (ops[i].kind == '=') {
			j++
		}
		run := ops[i:j]
		switch {
		case ops[i].kind != '=':
			out = append(out, run...)
		case i == 0 && len(run) > ctx: // context above the first change
			out = append(out, gap(len(run)-ctx, " above"))
			out = append(out, run[len(run)-ctx:]...)
		case j == len(ops) && len(run) > ctx: // context below the last one
			out = append(out, run[:ctx]...)
			out = append(out, gap(len(run)-ctx, " below"))
		case i > 0 && j < len(ops) && len(run) > 2*ctx: // between two changes
			out = append(out, run[:ctx]...)
			out = append(out, gap(len(run)-2*ctx, ""))
			out = append(out, run[len(run)-ctx:]...)
		default:
			out = append(out, run...)
		}
		i = j
	}
	return out
}
