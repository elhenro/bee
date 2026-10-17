package tui

import (
	"fmt"
	"sync"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
)

// hiveSpawner returns a spawn func that puts each hive engine on its [swarm]
// model tier, sharing one provider per tier. A tier that cannot be built falls
// back to the session model and is reported once.
func hiveSpawner(planner *loop.Engine, notify func(string)) func(label string, tier config.ModelTier) (*loop.Engine, *session.Rollout, error) {
	tiers := hive.NewTiers(planner)
	var mu sync.Mutex
	warned := map[config.ModelTier]bool{}
	return func(label string, tier config.ModelTier) (*loop.Engine, *session.Rollout, error) {
		eng, roll, warn, err := tiers.Spawn(label, tier)
		if warn != "" {
			mu.Lock()
			first := !warned[tier]
			warned[tier] = true
			mu.Unlock()
			if first {
				notify(warn)
			}
		}
		return eng, roll, err
	}
}

// tierNotice names the worker and reviewer models when [swarm] routes them;
// empty when both stay on the session default.
func tierNotice(cfg config.Config) string {
	w, r := cfg.Swarm.Worker, cfg.Swarm.Reviewer
	if w.IsZero() && r.IsZero() {
		return ""
	}
	return fmt.Sprintf("hive: workers on %s, reviewers on %s", hive.TierLabel(cfg, w), hive.TierLabel(cfg, r))
}

// hiveThrottle holds worker starts while the machine is oversubscribed.
func hiveThrottle(cfg config.Config, notify func(string)) hive.Throttle {
	return hive.Throttle{
		MaxLoadRatio: cfg.Swarm.MaxLoadRatio,
		OnHold: func(l float64) {
			notify(fmt.Sprintf("hive: load %.1f/core over %.1f, holding a worker", l, cfg.Swarm.MaxLoadRatio))
		},
	}
}
