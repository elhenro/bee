package hive

import (
	"fmt"
	"sync"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
)

// TierLabel renders a tier as provider/model for notices, filling blanks from
// the session default.
func TierLabel(cfg config.Config, t config.ModelTier) string {
	c := t.Apply(cfg)
	return c.DefaultProvider + "/" + c.DefaultModel
}

// Tiers builds one provider per distinct model tier and reuses it, so N
// workers on one tier share a client instead of building N.
type Tiers struct {
	parent *loop.Engine
	mu     sync.Mutex
	built  map[config.ModelTier]llm.Provider
}

// NewTiers returns a tier cache spawning engines off parent.
func NewTiers(parent *loop.Engine) *Tiers {
	return &Tiers{parent: parent, built: map[config.ModelTier]llm.Provider{}}
}

// Spawn clones a worker engine like SpawnWorker and points it at tier. A zero
// tier keeps the parent's provider and cfg. When the tier's provider cannot be
// built (no ProviderFor, bad config, missing key) the engine falls back to the
// parent's and warn says why; the run goes on rather than failing on routing.
//
// The tool registry stays the parent's: its one provider-bound tool, knowledge
// search, carries its own provider/model pair and keeps working unchanged.
func (t *Tiers) Spawn(label string, tier config.ModelTier) (eng *loop.Engine, roll *session.Rollout, warn string, err error) {
	eng, roll, err = SpawnWorker(t.parent, label)
	if err != nil || tier.IsZero() {
		return eng, roll, "", err
	}
	prov, perr := t.provider(tier)
	if perr != nil {
		return eng, roll, fmt.Sprintf("hive: %s tier %s unavailable (%v), using session model", label, TierLabel(t.parent.Cfg, tier), perr), nil
	}
	eng.Provider = prov
	eng.Cfg = tier.Apply(t.parent.Cfg)
	return eng, roll, "", nil
}

func (t *Tiers) provider(tier config.ModelTier) (llm.Provider, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if p, ok := t.built[tier]; ok {
		return p, nil
	}
	if t.parent.ProviderFor == nil {
		return nil, fmt.Errorf("no provider builder")
	}
	p, err := t.parent.ProviderFor(tier.Apply(t.parent.Cfg))
	if err != nil {
		return nil, err
	}
	t.built[tier] = p
	return p, nil
}

// SpawnTier is a one-off Tiers.Spawn without a shared cache.
func SpawnTier(parent *loop.Engine, label string, tier config.ModelTier) (*loop.Engine, *session.Rollout, string, error) {
	if parent == nil {
		return nil, nil, "", errNilEngine
	}
	return NewTiers(parent).Spawn(label, tier)
}
