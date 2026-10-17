package hive

import (
	"errors"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
)

// tierProv is a distinguishable provider stub; only identity matters here.
type tierProv struct {
	llm.Provider
	name string
}

func tierParent(t *testing.T) (*loop.Engine, *int) {
	t.Helper()
	t.Setenv("BEE_HOME", t.TempDir())
	t.Setenv("BEE_SESSIONS_DIR", t.TempDir())
	builds := 0
	cfg := config.Config{DefaultProvider: "base", DefaultModel: "big"}
	parent := &loop.Engine{Cfg: cfg, Cwd: "/p", Provider: &tierProv{name: "parent"}}
	parent.ProviderFor = func(c config.Config) (llm.Provider, error) {
		builds++
		if c.DefaultProvider == "broken" {
			return nil, errors.New("no key")
		}
		return &tierProv{name: c.DefaultProvider + "/" + c.DefaultModel}, nil
	}
	return parent, &builds
}

func TestSpawnTier_ZeroTierKeepsParent(t *testing.T) {
	parent, builds := tierParent(t)
	eng, roll, warn, err := SpawnTier(parent, "w", config.ModelTier{})
	if err != nil || warn != "" {
		t.Fatalf("err=%v warn=%q", err, warn)
	}
	defer roll.Close()
	if eng.Provider != parent.Provider || eng.Cfg.DefaultModel != "big" || *builds != 0 {
		t.Fatalf("zero tier must reuse parent provider and cfg, builds=%d", *builds)
	}
}

func TestSpawnTier_SetTierBuildsProviderAndCfg(t *testing.T) {
	parent, _ := tierParent(t)
	eng, roll, warn, err := SpawnTier(parent, "w", config.ModelTier{Provider: "local", Model: "tiny"})
	if err != nil || warn != "" {
		t.Fatalf("err=%v warn=%q", err, warn)
	}
	defer roll.Close()
	if p, ok := eng.Provider.(*tierProv); !ok || p.name != "local/tiny" {
		t.Fatalf("provider = %+v, want local/tiny", eng.Provider)
	}
	if eng.Cfg.DefaultProvider != "local" || eng.Cfg.DefaultModel != "tiny" {
		t.Fatalf("cfg not applied: %s/%s", eng.Cfg.DefaultProvider, eng.Cfg.DefaultModel)
	}
	if parent.Cfg.DefaultModel != "big" {
		t.Fatal("tier must not mutate the parent cfg")
	}
}

func TestSpawnTier_ModelOnlyKeepsProvider(t *testing.T) {
	parent, _ := tierParent(t)
	eng, roll, _, _ := SpawnTier(parent, "w", config.ModelTier{Model: "small"})
	defer roll.Close()
	if eng.Cfg.DefaultProvider != "base" || eng.Cfg.DefaultModel != "small" {
		t.Fatalf("got %s/%s", eng.Cfg.DefaultProvider, eng.Cfg.DefaultModel)
	}
}

func TestSpawnTier_FallbackWarns(t *testing.T) {
	parent, _ := tierParent(t)
	eng, roll, warn, err := SpawnTier(parent, "w", config.ModelTier{Provider: "broken"})
	if err != nil {
		t.Fatal(err)
	}
	defer roll.Close()
	if eng.Provider != parent.Provider || eng.Cfg.DefaultProvider != "base" || !strings.Contains(warn, "unavailable") {
		t.Fatalf("broken tier must fall back with a warning, warn=%q", warn)
	}

	parent.ProviderFor = nil
	eng2, roll2, warn2, _ := SpawnTier(parent, "w", config.ModelTier{Model: "x"})
	defer roll2.Close()
	if eng2.Provider != parent.Provider || warn2 == "" {
		t.Fatal("nil ProviderFor must fall back with a warning")
	}
}

func TestTiers_CachesOneProviderPerTier(t *testing.T) {
	parent, builds := tierParent(t)
	tiers := NewTiers(parent)
	small := config.ModelTier{Provider: "local", Model: "tiny"}
	var provs []llm.Provider
	for i := 0; i < 3; i++ {
		eng, roll, _, err := tiers.Spawn("w", small)
		if err != nil {
			t.Fatal(err)
		}
		defer roll.Close()
		provs = append(provs, eng.Provider)
	}
	if *builds != 1 || provs[0] != provs[1] || provs[1] != provs[2] {
		t.Fatalf("builds=%d, want one shared provider per tier", *builds)
	}
	if _, roll, _, _ := tiers.Spawn("r", config.ModelTier{Provider: "remote", Model: "big"}); roll != nil {
		defer roll.Close()
	}
	if *builds != 2 {
		t.Fatalf("a distinct tier must build its own provider, builds=%d", *builds)
	}
}
