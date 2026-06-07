package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/cost"
)

func TestUsagePaneRendersPopulated(t *testing.T) {
	t.Setenv("BEE_USAGE_LOG", filepath.Join(t.TempDir(), "usage.jsonl"))
	cost.ResetUsageForTest()
	now := time.Now().UTC()
	cost.AppendUsage(cost.UsageRecord{Time: now.Add(-time.Hour), Provider: "openrouter", Model: "m1", Input: 100, Output: 50, USD: 0.01, CostReported: true})
	cost.AppendUsage(cost.UsageRecord{Time: now.Add(-2 * time.Hour), Provider: "ollama", Model: "m2", Input: 200, Output: 100})

	u := NewUsagePane(nil)
	u, _ = u.Update(ToggleUsagePaneMsg{})
	if !u.Open() {
		t.Fatal("pane should be open after toggle")
	}
	out := u.View(80, 24)
	if !strings.Contains(out, "Usage overview") {
		t.Errorf("missing title in output:\n%s", out)
	}
	if !strings.Contains(out, "by provider") {
		t.Errorf("missing provider breakdown:\n%s", out)
	}
	if !strings.Contains(out, "local") {
		t.Errorf("local provider should show 'local' cost:\n%s", out)
	}

	// window switching must not panic and should change the active tab.
	u, _ = u.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if u.window != win7d {
		t.Errorf("key '2' should select 7d window, got %d", u.window)
	}
	u, _ = u.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if u.Open() {
		t.Error("esc should close the pane")
	}
}

func TestUsagePaneEmptyFallback(t *testing.T) {
	t.Setenv("BEE_USAGE_LOG", filepath.Join(t.TempDir(), "none.jsonl"))
	cost.ResetUsageForTest()
	t.Setenv("BEE_LIFETIME_TOKENS", filepath.Join(t.TempDir(), "life.json"))
	cost.ResetLifetimeForTest()

	u := NewUsagePane(nil)
	u, _ = u.Update(ToggleUsagePaneMsg{})
	out := u.View(80, 24)
	if !strings.Contains(out, "Usage overview") {
		t.Errorf("empty pane should still render title:\n%s", out)
	}
	if !strings.Contains(out, "no per-call history") {
		t.Errorf("empty pane should show the fallback note:\n%s", out)
	}
}
