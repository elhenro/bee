package hive

import (
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/loop"
)

func TestSpawnWorker_InheritsWaggleWhenEnabled(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	t.Setenv("BEE_SESSIONS_DIR", t.TempDir())
	cfg := config.Config{}
	cfg.Waggle.Enabled = true
	parent := &loop.Engine{Cfg: cfg, Cwd: "/p"}
	w, roll, err := SpawnWorker(parent, "w1")
	if err != nil {
		t.Fatal(err)
	}
	defer roll.Close()
	if w.Waggle == nil {
		t.Error("worker should inherit the waggle miner when enabled")
	}
}

func TestSpawnWorker_NoWaggleWhenDisabled(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	t.Setenv("BEE_SESSIONS_DIR", t.TempDir())
	cfg := config.Config{}
	cfg.Waggle.Enabled = false
	parent := &loop.Engine{Cfg: cfg, Cwd: "/p"}
	w, roll, err := SpawnWorker(parent, "w2")
	if err != nil {
		t.Fatal(err)
	}
	defer roll.Close()
	if w.Waggle != nil {
		t.Error("worker must not get waggle when disabled")
	}
}
