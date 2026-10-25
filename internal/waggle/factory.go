package waggle

// ProjectManager builds the project-scoped procedure-memory manager for cwd. It
// observes read-only routes and crystallizes repeated ones under the project
// store. Returns nil on store error, leaving the feature inert.
func ProjectManager(cwd string) *Manager { return ProjectManagerLearning(cwd, false) }

// ProjectManagerLearning is ProjectManager with mutating-route mining switched
// by mutating. Only a caller that reports outcomes through MarkOutcome (an
// unattended commit-or-reset loop) should pass true: without marks no mutating
// window is ever verified, so the switch is inert anyway.
func ProjectManagerLearning(cwd string, mutating bool) *Manager {
	store, err := ProjectStore(cwd)
	if err != nil {
		return nil
	}
	return NewManager(store, ManagerConfig{Scope: ScopeProject, Mine: MineConfig{Mutating: mutating}})
}

// ProjectReplayer loads crystallized routes (project store for cwd plus the
// shared user store) into a predictive replayer, wiring each scope's reuse
// ledger. Returns nil when no routes exist or the stores can't be resolved, so
// the loop runs unchanged on a cold library. Each call builds an independent
// replayer over the shared on-disk library, so concurrent agents (e.g. hive
// workers) never share live match state.
func ProjectReplayer(cwd string) *Replayer {
	stores := map[Scope]*Store{}
	if s, err := ProjectStore(cwd); err == nil {
		stores[ScopeProject] = s
	}
	if s, err := UserStore(); err == nil {
		stores[ScopeUser] = s
	}
	var routes []Route
	for _, scope := range []Scope{ScopeProject, ScopeUser} { // project routes match first
		if s := stores[scope]; s != nil {
			routes = append(routes, scopedRoutes(s, scope)...)
		}
	}
	if len(routes) == 0 {
		return nil
	}
	r := NewReplayer(routes, 2)
	for scope, s := range stores {
		r.SetLedger(scope, NewLedger(s.LedgerPath()))
	}
	return r
}

// scopedRoutes loads a store's routes and tags each with its scope so the
// replayer records reuse against the right ledger.
func scopedRoutes(s *Store, scope Scope) []Route {
	rs, err := LoadRoutes(s)
	if err != nil {
		return nil
	}
	for i := range rs {
		rs[i].Scope = scope
	}
	return rs
}
