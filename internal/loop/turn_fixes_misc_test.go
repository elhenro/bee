package loop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

func TestFirstUserText_SkipsSyntheticTurns(t *testing.T) {
	msgs := []types.Message{
		mkMsg(types.RoleUser, "[compacted history]\nold summary"),
		mkMsg(types.RoleUser, "[nudge] try again"),
		mkMsg(types.RoleUser, "[resume] Continue from where you left off."),
		mkMsg(types.RoleUser, "fix the bug"),
	}
	if got := firstUserText(msgs); got != "fix the bug" {
		t.Errorf("firstUserText = %q", got)
	}
}

func TestCardGoalFor_KeepsGoalAcrossResume(t *testing.T) {
	e := &Engine{}
	if g := e.cardGoalFor("build the thing", nil); g != "build the thing" {
		t.Fatalf("goal = %q", g)
	}
	if g := e.cardGoalFor(resumeContinueMsg, nil); g != "build the thing" {
		t.Errorf("resume replaced goal with %q", g)
	}
	fresh := &Engine{}
	prior := []types.Message{mkMsg(types.RoleUser, "original ask")}
	if g := fresh.cardGoalFor(resumeContinueMsg, prior); g != "original ask" {
		t.Errorf("fresh engine goal = %q, want seeded from transcript", g)
	}
}

func TestRescaleProfile_RevertsOnSmallerWindow(t *testing.T) {
	llm.RememberContextLength("big-local", 128_000)
	llm.RememberContextLength("small-local", 8_000)
	t.Cleanup(llm.ResetLiveContextLengths)
	cfg := config.Defaults()
	cfg.DefaultProvider = "ollama"
	cfg.Profile = "tiny"
	base := cfg.Profiles["tiny"]
	e := &Engine{Cfg: cfg}

	e.Cfg.DefaultModel = "big-local"
	e.rescaleProfile()
	if e.Cfg.Profiles["tiny"].SystemPromptBudget <= base.SystemPromptBudget {
		t.Fatal("large window should widen the tiny budget")
	}
	e.Cfg.DefaultModel = "small-local"
	e.rescaleProfile()
	got := e.Cfg.Profiles["tiny"]
	if got.SystemPromptBudget != base.SystemPromptBudget || got.ToolOutputTokens != base.ToolOutputTokens {
		t.Errorf("budgets did not revert: %+v want %+v", got, base)
	}
	if cfg.Profiles["tiny"].SystemPromptBudget != base.SystemPromptBudget {
		t.Error("caller's profile map was mutated")
	}
}

func TestSysPromptCacheKey_SeesSameLengthEdits(t *testing.T) {
	k := func(manifest string) string {
		return sysPromptCacheKey(config.Config{}, "", RoleWorker, false, nil, manifest, nil, nil)
	}
	if k("skill-a") == k("skill-b") {
		t.Error("same-length manifest edit must change the key")
	}
	if k("skill-a") != k("skill-a") {
		t.Error("key must be stable for identical input")
	}
}

func TestDescribeCached_DoesNotCacheFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()
	e := &Engine{
		Cfg: config.Config{
			DefaultModel: "deepseek-v4-flash",
			Vision:       config.VisionConfig{Endpoint: srv.URL, Model: "v"},
		},
		run: &runState{visionCache: map[string]string{}},
	}
	out := e.applyVisionFallback(context.Background(), imgMsg())
	if !strings.Contains(out[0].Content[1].Text, "failed") {
		t.Fatalf("expected failure text, got %q", out[0].Content[1].Text)
	}
	if len(e.run.visionCache) != 0 {
		t.Errorf("failed describe was cached: %v", e.run.visionCache)
	}
}
