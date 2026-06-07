package waggle

import (
	"sort"
	"strings"
)

// MineConfig bounds the search. MinLen/MaxLen cap route length; K is the minimum
// number of non-overlapping occurrences before a route is worth crystallizing.
type MineConfig struct {
	MinLen int
	MaxLen int
	K      int
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
	for l := cfg.MaxLen; l >= cfg.MinLen; l-- {
		if l > n {
			continue
		}
		groups := map[string][]int{}
		var order []string
		for i := 0; i+l <= n; i++ {
			win := calls[i : i+l]
			if windowHasMutator(win) {
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

func windowHasMutator(win []Call) bool {
	for _, c := range win {
		if c.Mutates {
			return true
		}
	}
	return false
}

// shapeSig keys a window by tool + sorted arg keys (not values), so calls that
// differ only in argument values share a shape and form a parameterizable family.
func shapeSig(win []Call) string {
	var b strings.Builder
	for _, c := range win {
		b.WriteString(c.Tool)
		b.WriteByte('(')
		b.WriteString(strings.Join(sortedKeys(c.Args), ","))
		b.WriteString(");")
	}
	return b.String()
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
	for s := 0; s < l; s++ {
		steps[s] = cloneCall(rep[s])
	}
	var params []Param
	for s := 0; s < l; s++ {
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
	out := c
	if c.Args != nil {
		out.Args = make(map[string]string, len(c.Args))
		for k, v := range c.Args {
			out.Args[k] = v
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
