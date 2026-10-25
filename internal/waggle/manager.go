package waggle

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// ManagerConfig tunes the live crystallization loop. Mine bounds detection;
// Scope tags written waggles; MinePeriod is how many observations between sweeps
// (default 8) so mining never runs on the hot path of every single tool call.
type ManagerConfig struct {
	Mine       MineConfig
	Scope      Scope
	MinePeriod int
}

// Manager ties the recorder, miner and promoter to a store. The loop feeds it
// every read-only tool call via Observe; it periodically mines the forage log
// and writes one new crystallizable waggle per sweep. All work is read-only and
// best-effort: a failure never disrupts the turn.
type Manager struct {
	rec    *Recorder
	store  *Store
	cfg    ManagerConfig
	period int
	mu     sync.Mutex
	since  int
}

// NewManager returns a manager writing to store. A nil store yields a no-op
// manager so callers need not branch.
func NewManager(store *Store, cfg ManagerConfig) *Manager {
	period := cfg.MinePeriod
	if period <= 0 {
		period = 8
	}
	return &Manager{rec: NewRecorder(0), store: store, cfg: cfg, period: period}
}

// Observe records a call and triggers a sweep every period observations.
func (m *Manager) Observe(c Call) {
	if m == nil || m.store == nil {
		return
	}
	m.rec.Record(c)
	m.mu.Lock()
	m.since++
	due := m.since >= m.period
	if due {
		m.since = 0
	}
	m.mu.Unlock()
	if due {
		m.sweep()
	}
}

// MarkOutcome closes the forage span since the last mark: ok verifies those
// calls for mutating-route mining, !ok discards them. Unattended loops call it
// after commit-or-reset; a manager that never hears it mines read-only routes
// only.
func (m *Manager) MarkOutcome(ok bool) {
	if m == nil || m.store == nil {
		return
	}
	m.rec.MarkOutcome(ok)
}

// sweep mines the current forage log and promotes the single highest-value
// crystallizable route, returning how many it wrote (0 or 1). If that route is
// already stored the sweep is a no-op: it does not fall through to lesser or
// phase-shifted routes, which prevents duplicate and rotated near-copies. A
// genuinely distinct route is promoted once it becomes the top candidate.
func (m *Manager) sweep() int {
	for _, c := range Mine(m.rec.Calls(), m.cfg.Mine) {
		name, md, ok := render(c, m.cfg.Scope)
		if !ok {
			continue // not crystallizable; keep looking for the best one that is
		}
		if m.store.Exists(name) {
			return 0 // best route already captured; nothing new to promote
		}
		if err := m.store.Write(name, md); err != nil {
			return 0
		}
		return 1
	}
	return 0
}

// render picks the on-disk form for a candidate: a route with a mutating step
// becomes a recipe waggle (run only by `bee <name>`), a read-only route an exec
// waggle (replayable). The name hashes the rendered script either way.
func render(c Candidate, scope Scope) (name, md string, ok bool) {
	var script string
	renderFn := Render
	if hasMutator(c.Steps) {
		script, _, ok = recipeCmds(c)
		renderFn = RenderRecipe
	} else {
		script, ok = scriptOf(c)
	}
	if !ok {
		return "", "", false
	}
	name = waggleName(script)
	md, ok = renderFn(name, c, scope)
	return name, md, ok
}

// waggleName derives a stable name from the route's script, so the same route
// always maps to the same file (idempotent promotion).
func waggleName(script string) string {
	sum := sha256.Sum256([]byte(script))
	return "wag_" + hex.EncodeToString(sum[:])[:10]
}
