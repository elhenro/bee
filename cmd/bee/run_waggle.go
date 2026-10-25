package main

import (
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/sandbox"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/tools/waggle_lookup"
	"github.com/elhenro/bee/internal/waggle"
)

// waggleManager builds the project-scoped procedure-memory miner for cwd, or
// nil when waggle is disabled in config. Gating here keeps every engine — run,
// tui, and queen-spawned hive workers — consistent on one setting.
func waggleManager(cfg config.Config, cwd string) *waggle.Manager {
	if !cfg.Waggle.Enabled {
		return nil
	}
	return waggle.ProjectManager(cwd)
}

// waggleManagerLearning is waggleManager with mutating-route mining on. Only
// loops that verify outcomes (zzz commit-or-reset) wire it; the routes it
// writes are recipe waggles, run only by `bee <name>`.
func waggleManagerLearning(cfg config.Config, cwd string) *waggle.Manager {
	if !cfg.Waggle.Enabled {
		return nil
	}
	return waggle.ProjectManagerLearning(cwd, true)
}

// waggleReplayer builds the predictive replayer for cwd, or nil when waggle is
// disabled or the library is cold.
func waggleReplayer(cfg config.Config, cwd string) *waggle.Replayer {
	if !cfg.Waggle.Enabled {
		return nil
	}
	return waggle.ProjectReplayer(cwd)
}

// appendWaggleLookup registers the procedure-memory lookup tool iff waggle is
// enabled and the library holds at least one waggle. One manifest slot exposes
// the whole library on demand, so a cold install (no waggles yet) pays nothing
// and the cost never grows with library size.
func appendWaggleLookup(all []tools.Tool, cfg config.Config, cwd string) []tools.Tool {
	if !cfg.Waggle.Enabled {
		return all
	}
	proj, _ := waggle.ProjectStore(cwd)
	user, _ := waggle.UserStore()
	if waggleLibraryEmpty(proj) && waggleLibraryEmpty(user) {
		return all
	}
	if t := waggle_lookup.New(proj, user); t != nil {
		all = append(all, t.Confine(sandbox.Scope(cfg.Sandbox.Scope).Confines()).WithOptions(execSandbox(cfg, cwd)...))
	}
	return all
}

func waggleLibraryEmpty(s *waggle.Store) bool {
	if s == nil {
		return true
	}
	metas, _ := waggle.List(s)
	return len(metas) == 0
}
