package waggle

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	yaml "github.com/goccy/go-yaml"
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
		r.seq = append(r.seq[:0:0], r.seq[len(r.seq)-replaySeqCap:]...)
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
	if err != nil || strings.TrimSpace(out) == "" {
		// divergence: the prefix matched but the stored tail no longer runs
		// clean. Record it so curation can demote a route the tree outgrew.
		r.mu.Lock()
		ledger := r.ledgers[plan.scope]
		r.mu.Unlock()
		_ = ledger.Append(LedgerEntry{Name: plan.name, Steps: plan.steps, Fail: true})
		return "", false
	}
	gain := len(out) / 4
	r.mu.Lock()
	r.yield += gain
	ledger := r.ledgers[plan.scope]
	r.mu.Unlock()
	_ = ledger.Append(LedgerEntry{Name: plan.name, Steps: plan.steps, Yield: gain})
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
		pset := paramSet(rt.Params)
		for j := r.minPrefix; j < total; j++ {
			if j > n {
				break
			}
			if !prefixMatch(rt, pset, r.seq[n-j:]) {
				continue
			}
			if !tailLiteral(rt, j) {
				continue
			}
			script, _, ok := scriptOf(Candidate{Steps: rt.Steps[j:]})
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
func prefixMatch(rt Route, pset map[string]bool, calls []Call) bool {
	if len(calls) > len(rt.Steps) {
		return false // caller aligns calls to a prefix; guard the index invariant
	}
	for i, c := range calls {
		step := rt.Steps[i]
		if step.Tool != c.Tool || !sameKeys(step.Args, c.Args) {
			return false
		}
		for k, v := range step.Args {
			if pset[paramKey(i, k)] {
				continue // param position: any value is fine
			}
			if c.Args[k] != v {
				return false
			}
		}
	}
	return true
}

// tailLiteral reports whether every step from index j on is fully literal (no
// param references), so the remaining route is deterministic without knowing
// what the model would have passed next.
func tailLiteral(rt Route, j int) bool {
	for _, p := range rt.Params {
		if p.Step >= j {
			return false
		}
	}
	return true
}

func paramSet(params []Param) map[string]bool {
	if len(params) == 0 {
		return nil
	}
	m := make(map[string]bool, len(params))
	for _, p := range params {
		m[paramKey(p.Step, p.Key)] = true
	}
	return m
}

func paramKey(step int, key string) string { return strconv.Itoa(step) + "\x00" + key }

func sameKeys(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
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
	ents, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Route
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if rt, ok := loadRouteFile(filepath.Join(s.dir, e.Name())); ok {
			out = append(out, rt)
		}
	}
	return out, nil
}

func loadRouteFile(path string) (Route, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Route{}, false
	}
	fm, ok := frontmatterBytes(raw)
	if !ok {
		return Route{}, false
	}
	var meta struct {
		Name     string      `yaml:"name"`
		Disabled bool        `yaml:"disabled"`
		Route    []routeStep `yaml:"route"`
	}
	if err := yaml.Unmarshal(fm, &meta); err != nil || len(meta.Route) == 0 {
		return Route{}, false
	}
	if meta.Disabled {
		return Route{}, false // demoted by curation; keep the file, skip replay
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

// frontmatterBytes returns the YAML between the leading `---` fences.
func frontmatterBytes(raw []byte) ([]byte, bool) {
	s := strings.TrimLeft(string(raw), " \t\r\n")
	if !strings.HasPrefix(s, "---") {
		return nil, false
	}
	nl := strings.IndexByte(s, '\n')
	if nl < 0 {
		return nil, false
	}
	rest := s[nl+1:]
	for start := 0; start < len(rest); {
		end := strings.IndexByte(rest[start:], '\n')
		var line string
		if end < 0 {
			line = rest[start:]
		} else {
			line = rest[start : start+end]
		}
		if strings.TrimRight(line, "\r ") == "---" {
			return []byte(rest[:start]), true
		}
		if end < 0 {
			break
		}
		start += end + 1
	}
	return nil, false
}
