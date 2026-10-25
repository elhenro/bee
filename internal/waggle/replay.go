package waggle

import (
	"context"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// replaySeqCap bounds the live call window the replayer matches against, so a
// long session never grows the match cost without bound.
const replaySeqCap = 32

// Route is a stored waggle rehydrated for predictive replay: the ordered steps
// (literal args carry values; param args are wildcards) plus which positions are
// params. Steps mirror the miner's Candidate so translation is shared.
type Route struct {
	Name   string
	Steps  []Call
	Params []Param
	// Scope attributes a fired route to its store's ledger. Defaults to project.
	Scope Scope
}

// Replayer follows known routes. It watches the live read-only call stream and,
// when the most recent calls match the start of a stored route whose remaining
// steps are fully determined (no unbound params), runs those remaining steps
// deterministically off the model's path and returns their combined output to
// fold into the triggering tool result. Everything is read-only, so a wrong
// match wastes a little read work and never causes damage.
type Replayer struct {
	mu        sync.Mutex
	routes    []Route
	seq       []Call
	minPrefix int
	yield     int
	fired     map[string]bool
	ledgers   map[Scope]*Ledger
}

// NewReplayer builds a replayer over routes. minPrefix is the smallest number of
// matched leading steps before a route fires (clamped to >= 2) — the
// conservatism knob that stops one ubiquitous call from hijacking exploration.
func NewReplayer(routes []Route, minPrefix int) *Replayer {
	if minPrefix < 2 {
		minPrefix = 2
	}
	return &Replayer{routes: routes, minPrefix: minPrefix, fired: map[string]bool{}, ledgers: map[Scope]*Ledger{}}
}

// SetLedger registers the reuse log for a scope. Routes of that scope record an
// entry each time they fire. A nil ledger (or unset scope) just skips logging.
func (r *Replayer) SetLedger(scope Scope, l *Ledger) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ledgers[scope] = l
}

// Routes returns how many routes the replayer holds (0 for a nil/empty one).
func (r *Replayer) Routes() int {
	if r == nil {
		return 0
	}
	return len(r.routes)
}

// Observe appends a completed read-only call to the live window.
func (r *Replayer) Observe(c Call) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq = append(r.seq, c)
	if len(r.seq) > replaySeqCap {
		r.seq = slices.Clone(r.seq[len(r.seq)-replaySeqCap:])
	}
}

// Yield returns the cumulative estimated tokens saved by replays this session.
func (r *Replayer) Yield() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.yield
}

// Follow checks the current window for a fireable route and, if found, runs its
// remaining steps via exec (which must apply the host's safety + truncation).
// It returns the formatted block to append to the triggering tool result and
// true, or "" and false when nothing fires. Best-effort: an exec error or empty
// output yields no block. A given concrete plan fires at most once per session.
func (r *Replayer) Follow(ctx context.Context, exec func(context.Context, string) (string, error)) (string, bool) {
	if r == nil || exec == nil {
		return "", false
	}
	r.mu.Lock()
	plan, ok := r.matchLocked()
	if !ok || r.fired[plan.key] {
		r.mu.Unlock()
		return "", false
	}
	r.fired[plan.key] = true // mark before exec so a failing route can't loop
	r.mu.Unlock()

	out, err := exec(ctx, plan.script)
	// divergence: the prefix matched but the stored tail no longer runs clean.
	// Record it so curation can demote a route the tree outgrew.
	diverged := err != nil || strings.TrimSpace(out) == ""
	gain := 0
	if !diverged {
		gain = len(out) / 4
	}
	r.mu.Lock()
	r.yield += gain
	ledger := r.ledgers[plan.scope]
	r.mu.Unlock()
	_ = ledger.Append(LedgerEntry{Name: plan.name, Steps: plan.steps, Yield: gain, Fail: diverged})
	if diverged {
		return "", false
	}
	return formatBlock(plan, out), true
}

// replayPlan is a matched, ready-to-run continuation.
type replayPlan struct {
	name   string
	script string
	steps  int
	key    string
	scope  Scope
}

// matchLocked finds the first route whose leading steps align to the tail of the
// live window (literals exact, params wildcard) and whose remaining steps carry
// no unbound params, so they translate to a fixed shell script. Caller holds mu.
func (r *Replayer) matchLocked() (replayPlan, bool) {
	n := len(r.seq)
	for _, rt := range r.routes {
		total := len(rt.Steps)
		for j := r.minPrefix; j < min(total, n+1); j++ {
			if !prefixMatch(rt, r.seq[n-j:]) || !tailLiteral(rt, j) {
				continue
			}
			script, ok := scriptOf(Candidate{Steps: rt.Steps[j:]})
			if !ok {
				continue
			}
			return replayPlan{name: rt.Name, script: script, steps: total - j, key: rt.Name + "|" + script, scope: rt.Scope}, true
		}
	}
	return replayPlan{}, false
}

// prefixMatch reports whether calls equal the route's first len(calls) steps:
// same tool, same arg-key set, and equal values on literal (non-param) keys.
func prefixMatch(rt Route, calls []Call) bool {
	if len(calls) > len(rt.Steps) {
		return false // caller aligns calls to a prefix; guard the index invariant
	}
	anyValue := func(_, _ string) bool { return true }
	for i, c := range calls {
		step := rt.Steps[i]
		if step.Tool != c.Tool || !maps.EqualFunc(step.Args, c.Args, anyValue) {
			return false
		}
		for k, v := range step.Args {
			if c.Args[k] != v && !slices.Contains(rt.Params, Param{Step: i, Key: k}) {
				return false // a param position accepts any value
			}
		}
	}
	return true
}

// tailLiteral reports whether every step from index j on is fully literal (no
// param references), so the remaining route is deterministic without knowing
// what the model would have passed next.
func tailLiteral(rt Route, j int) bool {
	return !slices.ContainsFunc(rt.Params, func(p Param) bool { return p.Step >= j })
}

func formatBlock(p replayPlan, out string) string {
	return "\n\n[waggle " + p.name + " prefetched " + strconv.Itoa(p.steps) +
		" known step(s) of this route]\n$ " + p.script + "\n" + out
}

var paramTokenRe = regexp.MustCompile(`^\$(\d+)$`)

// LoadRoutes reads every replayable waggle in a store. A missing dir is not an
// error (returns nil). Files without a structured route block are skipped (they
// still work as exec-skills, just aren't replayed).
func LoadRoutes(s *Store) ([]Route, error) {
	names, err := s.mdNames()
	var out []Route
	for _, n := range names {
		if rt, ok := loadRouteFile(filepath.Join(s.dir, n)); ok {
			out = append(out, rt)
		}
	}
	return out, err
}

func loadRouteFile(path string) (Route, bool) {
	var meta struct {
		Name     string      `yaml:"name"`
		Disabled bool        `yaml:"disabled"`
		Route    []routeStep `yaml:"route"`
	}
	// a demoted route keeps its file but is skipped by replay
	if readFrontmatter(path, &meta) != nil || len(meta.Route) == 0 || meta.Disabled {
		return Route{}, false
	}
	if meta.Name == "" {
		meta.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return routeFromSteps(meta.Name, meta.Route), true
}

// routeFromSteps inverts routeOf: "$N" args become Params (ordered by N) and the
// key is kept in Steps so shape matching still sees it; other args are literals.
func routeFromSteps(name string, steps []routeStep) Route {
	r := Route{Name: name}
	type ord struct {
		n int
		p Param
	}
	var ords []ord
	for s, st := range steps {
		args := make(map[string]string, len(st.Args))
		for k, v := range st.Args {
			if m := paramTokenRe.FindStringSubmatch(v); m != nil {
				n, _ := strconv.Atoi(m[1])
				ords = append(ords, ord{n, Param{Step: s, Key: k}})
			}
			args[k] = v
		}
		r.Steps = append(r.Steps, Call{Tool: st.Tool, Args: args})
	}
	sort.Slice(ords, func(i, j int) bool { return ords[i].n < ords[j].n })
	for _, o := range ords {
		r.Params = append(r.Params, o.p)
	}
	return r
}
