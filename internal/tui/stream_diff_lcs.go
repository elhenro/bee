package tui

import "fmt"

// editOp is one step in a line-level diff: kind ∈ {'=','+','-','~'} where
// '~' marks a synthetic gap line ("⋯ +K unchanged") emitted by the
// collapser. text is the line content (or marker payload for '~').
type editOp struct {
	kind byte
	text string
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
			out = append(out, editOp{'-', l})
		}
		for _, l := range b {
			out = append(out, editOp{'+', l})
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
			out = append(out, editOp{'=', a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, editOp{'-', a[i]})
			i++
		default:
			out = append(out, editOp{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, editOp{'-', a[i]})
	}
	for ; j < m; j++ {
		out = append(out, editOp{'+', b[j]})
	}
	return out
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
			out = append(out, editOp{'~', fmt.Sprintf("+%d unchanged above", hidden)})
			gap = gap[len(gap)-ctx:]
		case idx > 0 && len(gap) > 2*ctx:
			hidden := len(gap) - 2*ctx
			out = append(out, gap[:ctx]...)
			out = append(out, editOp{'~', fmt.Sprintf("+%d unchanged", hidden)})
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
		out = append(out, editOp{'~', fmt.Sprintf("+%d unchanged below", hidden)})
	} else {
		out = append(out, tail...)
	}
	return out
}

// balancedTrim keeps the first `keep` lines but, when the slice starts with
// a long run of `-`-styled deletions, reserves the back half of the budget
// for whatever follows so additions/context don't get squeezed out. Style
// detection is heuristic: the diffSign output always begins with the styled
// sign char (`+`/`-`) inside an ANSI escape, so we walk the visible bytes
// after `\x1b[…m` for the first printable byte.
func balancedTrim(lines []string, keep int) []string {
	if keep <= 0 || len(lines) <= keep {
		return lines
	}
	leadDel := 0
	for _, l := range lines {
		if firstVisible(l) != '-' {
			break
		}
		leadDel++
	}
	if leadDel <= keep/2 {
		return lines[:keep]
	}
	half := keep / 2
	tailNeed := keep - half
	out := make([]string, 0, keep)
	out = append(out, lines[:half]...)
	out = append(out, lines[len(lines)-tailNeed:]...)
	return out
}

// firstVisible returns the first non-ANSI printable byte of s, or 0 if
// none. Walks past `\x1b[…m` escape sequences without parsing them — only
// the m terminator matters. Used by balancedTrim to peek at the sign of a
// styled diff line.
func firstVisible(s string) byte {
	i := 0
	for i < len(s) {
		if s[i] != 0x1b {
			return s[i]
		}
		i++
		if i < len(s) && s[i] == '[' {
			i++
		}
		for i < len(s) && s[i] != 'm' {
			i++
		}
		if i < len(s) {
			i++
		}
	}
	return 0
}
