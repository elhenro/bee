package waggle

import (
	"maps"
	"slices"
	"sort"
	"strings"
)

// MineConfig bounds the search. MinLen/MaxLen cap route length; K is the minimum
// number of non-overlapping occurrences before a route is worth crystallizing.
// Mutating admits windows that contain mutating calls, provided every call in
// the window is Verified and every mutator is a shell command; such a route
// crystallizes as a recipe (explicit, approval-gated), never as a replayable
// exec waggle.
type MineConfig struct {
	MinLen   int
	MaxLen   int
	K        int
	Mutating bool
}

// Param marks an argument position whose value varies across occurrences, so it
// becomes a parameter of the crystallized route rather than a literal.
type Param struct {
	Step int
	Key  string
}

// Candidate is a detected reusable route. Steps holds the representative (first)
// occurrence; Params lists the varying argument positions; Count is the number
// of non-overlapping times the route's shape recurred.
type Candidate struct {
	Steps  []Call
	Count  int
	Params []Param
}

// Mine scans calls for repeated contiguous read-only routes, returning
// candidates ranked by score (length * count) descending. Windows containing a
// mutator are never considered. Callers typically act on the top candidate.
func Mine(calls []Call, cfg MineConfig) []Candidate {
	if cfg.MinLen <= 0 {
		cfg.MinLen = 2
	}
	if cfg.MaxLen <= 0 {
		cfg.MaxLen = 6
	}
	if cfg.K <= 0 {
		cfg.K = 2
	}
	n := len(calls)
	var out []Candidate
	seen := map[string]bool{}
	for l := min(cfg.MaxLen, n); l >= cfg.MinLen; l-- {
		groups := map[string][]int{}
		var order []string
		for i := 0; i+l <= n; i++ {
			win := calls[i : i+l]
			if hasMutator(win) && (!cfg.Mutating || !verifiedProcedure(win)) {
				continue
			}
			if !isPrimitive(win) {
				continue
			}
			sig := shapeSig(win)
			if _, ok := groups[sig]; !ok {
				order = append(order, sig)
			}
			groups[sig] = append(groups[sig], i)
		}
		for _, sig := range order {
			if seen[sig] {
				continue
			}
			starts := groups[sig]
			count := nonOverlapCount(starts, l)
			if count < cfg.K {
				continue
			}
			seen[sig] = true
			windows := make([][]Call, 0, len(starts))
			for _, s := range starts {
				windows = append(windows, calls[s:s+l])
			}
			c := buildCandidate(windows, l)
			c.Count = count
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return len(out[i].Steps)*out[i].Count > len(out[j].Steps)*out[j].Count
	})
	return out
}

// verifiedProcedure reports whether a window with mutators may be mined: every
// call verified by an outcome mark, and every mutator a shell command. Edits
// and writes carry file contents, which are task data rather than procedure.
func verifiedProcedure(win []Call) bool {
	for _, c := range win {
		if !c.Verified || (c.Mutates && c.Tool != "bash") {
			return false
		}
	}
	return true
}

// callShape is the value-independent identity of a call: tool + sorted arg keys.
// A shell call is identified by its command: two different commands are two
// different steps, not one parameterizable shape.
func callShape(c Call) string {
	if c.Tool == "bash" {
		return "bash{" + c.Args["command"] + "}"
	}
	return c.Tool + "(" + strings.Join(sortedKeys(c.Args), ",") + ")"
}

// shapeSig keys a window by its sequence of call shapes, so calls that differ
// only in argument values share a shape and form a parameterizable family.
func shapeSig(win []Call) string {
	var b strings.Builder
	for _, c := range win {
		b.WriteString(callShape(c))
		b.WriteByte(';')
	}
	return b.String()
}

// isPrimitive reports whether a window's shape sequence is not a whole-number
// repetition of a shorter sub-route. Non-primitive windows (e.g. ls,read,ls,read)
// are skipped so only the primitive route (ls,read) is crystallized.
func isPrimitive(win []Call) bool {
	l := len(win)
	toks := make([]string, l)
	for i, c := range win {
		toks[i] = callShape(c)
	}
	for p := 1; p < l; p++ {
		if l%p != 0 {
			continue
		}
		periodic := true
		for i := p; i < l; i++ {
			if toks[i] != toks[i-p] {
				periodic = false
				break
			}
		}
		if periodic {
			return false
		}
	}
	return true
}

// nonOverlapCount counts how many occurrences fit without overlapping, so a
// single call repeated back-to-back cannot inflate a short route's tally.
func nonOverlapCount(starts []int, l int) int {
	count, lastEnd := 0, -1
	for _, s := range starts {
		if s >= lastEnd {
			count++
			lastEnd = s + l
		}
	}
	return count
}

func buildCandidate(windows [][]Call, l int) Candidate {
	rep := windows[0]
	steps := make([]Call, l)
	for s := range l {
		steps[s] = cloneCall(rep[s])
	}
	var params []Param
	for s := range l {
		for _, k := range sortedKeys(rep[s].Args) {
			v0 := rep[s].Args[k]
			for _, w := range windows[1:] {
				if w[s].Args[k] != v0 {
					params = append(params, Param{Step: s, Key: k})
					break
				}
			}
		}
	}
	return Candidate{Steps: steps, Params: params}
}

func cloneCall(c Call) Call {
	c.Args = maps.Clone(c.Args)
	return c
}

func sortedKeys(m map[string]string) []string { return slices.Sorted(maps.Keys(m)) }
