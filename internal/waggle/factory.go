package waggle

// ProjectManager builds the project-scoped procedure-memory manager for cwd. It
// observes read-only routes and crystallizes repeated ones under the project
// store. Returns nil on store error, leaving the feature inert.
func ProjectManager(cwd string) *Manager {
	store, err := ProjectStore(cwd)
	if err != nil {
		return nil
	}
	return NewManager(store, ManagerConfig{Scope: ScopeProject})
}

// ProjectReplayer loads crystallized routes (project store for cwd plus the
// shared user store) into a predictive replayer, wiring each scope's reuse
// ledger. Returns nil when no routes exist or the stores can't be resolved, so
// the loop runs unchanged on a cold library. Each call builds an independent
// replayer over the shared on-disk library, so concurrent agents (e.g. hive
// workers) never share live match state.
func ProjectReplayer(cwd string) *Replayer {
	proj, errP := ProjectStore(cwd)
	user, errU := UserStore()
	var routes []Route
	if errP == nil {
		routes = append(routes, scopedRoutes(proj, ScopeProject)...)
	}
	if errU == nil {
		routes = append(routes, scopedRoutes(user, ScopeUser)...)
	}
	if len(routes) == 0 {
		return nil
	}
	r := NewReplayer(routes, 2)
	if errP == nil {
		r.SetLedger(ScopeProject, NewLedger(proj.LedgerPath()))
	}
	if errU == nil {
		r.SetLedger(ScopeUser, NewLedger(user.LedgerPath()))
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
