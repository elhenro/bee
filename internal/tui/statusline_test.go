package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
)

// statusTestModel builds a no-engine model large enough to fit the default
// line without clipping, so ordering assertions see every item.
func statusTestModel() Model {
	m := NewModel(nil, "/tmp/work", "gpt-4o-mini", "workspace-write", caveman.Default)
	m.width = 120
	return m
}

// AC: ordered config renders in that exact order.
func TestStatusLine_ConfigOrderRendersInOrder(t *testing.T) {
	m := statusTestModel().WithStatusLine([]string{"cwd", "model", "context"})
	out := stripANSI(m.renderTopBar())
	iCwd := strings.Index(out, "/tmp/work")
	iModel := strings.Index(out, "gpt-4o-mini")
	iContext := strings.Index(out, "🐝")
	if iCwd < 0 || iModel < 0 || iContext < 0 {
		t.Fatalf("missing item(s) in %q", out)
	}
	if !(iCwd < iModel && iModel < iContext) {
		t.Fatalf("order not preserved: cwd=%d model=%d context=%d in %q", iCwd, iModel, iContext, out)
	}
}

// A right-aligned item must not jump ahead of a left item that precedes it in
// config, and items after it stay ordered too. This is the mixed-side case the
// alignment-gap rule is designed to preserve.
func TestStatusLine_MixedAlignmentKeepsOrder(t *testing.T) {
	m := statusTestModel()
	m.lastTurnDuration = 3 * time.Second
	m = m.WithStatusLine([]string{"model", "turn_timer", "cwd"})
	out := stripANSI(m.renderTopBar())
	iModel := strings.Index(out, "gpt-4o-mini")
	iTimer := strings.Index(out, "3.0s")
	iCwd := strings.Index(out, "/tmp/work")
	if iModel < 0 || iTimer < 0 || iCwd < 0 {
		t.Fatalf("missing item(s) in %q", out)
	}
	if !(iModel < iTimer && iTimer < iCwd) {
		t.Fatalf("mixed order not preserved: model=%d timer=%d cwd=%d in %q", iModel, iTimer, iCwd, out)
	}
}

// AC: unknown id is ignored without breaking the rest.
func TestStatusLine_UnknownIDIgnored(t *testing.T) {
	m := statusTestModel().WithStatusLine([]string{"cwd", "not_a_real_id", "model"})
	out := stripANSI(m.renderTopBar())
	iCwd := strings.Index(out, "/tmp/work")
	iModel := strings.Index(out, "gpt-4o-mini")
	if iCwd < 0 || iModel < 0 {
		t.Fatalf("known items dropped alongside unknown id: %q", out)
	}
	if iCwd > iModel {
		t.Fatalf("order broken by unknown id: cwd=%d model=%d", iCwd, iModel)
	}
	if strings.Contains(out, "not_a_real_id") {
		t.Fatalf("unknown id leaked into render: %q", out)
	}
}

// Duplicates keep the first occurrence, so the item renders exactly once.
func TestStatusLine_DuplicateFirstWins(t *testing.T) {
	m := statusTestModel().WithStatusLine([]string{"cwd", "model", "cwd"})
	out := stripANSI(m.renderTopBar())
	if n := strings.Count(out, "/tmp/work"); n != 1 {
		t.Fatalf("cwd rendered %d times, want 1: %q", n, out)
	}
	if got := resolveStatusLine([]string{"cwd", "model", "cwd"}); len(got) != 2 || got[0] != "cwd" || got[1] != "model" {
		t.Fatalf("resolveStatusLine dedup = %v, want [cwd model]", got)
	}
}

// AC: empty array hides the line.
func TestStatusLine_EmptyListHidesLine(t *testing.T) {
	m := statusTestModel().WithStatusLine([]string{})
	if out := m.renderTopBar(); out != "" {
		t.Fatalf("empty status_line should hide the line, got %q", out)
	}
}

// AC: missing key uses defaults.
func TestStatusLine_MissingKeyUsesDefaults(t *testing.T) {
	m := statusTestModel() // statusLine left nil == key absent
	out := stripANSI(m.renderTopBar())
	if !strings.Contains(out, "🐝") || !strings.Contains(out, "/tmp/work") {
		t.Fatalf("default line missing expected items: %q", out)
	}
	if got := resolveStatusLine(nil); len(got) != len(defaultStatusLineIDs()) {
		t.Fatalf("nil config resolved to %v, want defaults", got)
	}
}

// AC: damaged config does not block startup — junk and blank ids degrade to a
// shorter line instead of panicking or erroring.
func TestStatusLine_DamagedConfigDoesNotPanic(t *testing.T) {
	m := statusTestModel().WithStatusLine([]string{"", "   ", "bogus", "cwd"})
	out := stripANSI(m.renderTopBar())
	if !strings.Contains(out, "/tmp/work") {
		t.Fatalf("valid id dropped by damaged list: %q", out)
	}
	// All-unknown list degrades to hidden, not a crash.
	m2 := statusTestModel().WithStatusLine([]string{"bogus", "also_bogus"})
	if got := m2.renderTopBar(); got != "" {
		t.Fatalf("all-unknown list should render empty, got %q", got)
	}
}

// AC: rendering is ANSI-width-safe.
func TestStatusLine_AnsiWidthSafe(t *testing.T) {
	m := statusTestModel()
	m.width = 24
	m = m.WithStatusLine([]string{
		"context", "model", "cwd", "git_branch", "cost", "total_tokens",
		"goal", "turn_timer", "yolo", "role", "effort",
	})
	out := m.renderTopBar()
	if w := lipglossWidth(out); w > 24 {
		t.Fatalf("line width %d exceeds terminal width 24: %q", w, out)
	}
}

// AC: at least the existing items are expressible.
func TestStatusLine_ExistingItemsExpressible(t *testing.T) {
	for _, id := range []string{
		"context", "model", "cwd", "git_branch", "session_title",
		"cost", "total_tokens", "goal", "turn_timer", "yolo", "role", "effort",
	} {
		it, ok := statusRegistry[id]
		if !ok {
			t.Errorf("registry missing id %q", id)
			continue
		}
		if it.draw == nil {
			t.Errorf("registry item %q has nil renderer", id)
		}
	}
}

// The session-title item names the session it belongs to, sourced from the
// attached rollout and shortened to a stable label.
func TestStatusLine_SessionTitleRenders(t *testing.T) {
	t.Setenv("BEE_SESSIONS_DIR", t.TempDir())
	roll, err := session.Open("abcdef1234567890")
	if err != nil {
		t.Fatalf("open rollout: %v", err)
	}
	defer roll.Close()
	eng := &loop.Engine{Sessions: roll}
	m := NewModel(eng, "/tmp/work", "gpt-4o-mini", "workspace-write", caveman.Default)
	m.width = 120
	m = m.WithStatusLine([]string{"session_title"})
	out := stripANSI(m.renderTopBar())
	if !strings.Contains(out, "abcdef12") {
		t.Fatalf("session title not rendered: %q", out)
	}
}

// Preserved behavior: on the default (unconfigured) path, flipping every
// user-facing chrome toggle off collapses the whole row.
func TestStatusLine_AllTogglesOffCollapsesDefault(t *testing.T) {
	m := statusTestModel()
	m.showBee, m.showContextPct, m.showModel = false, false, false
	m.showCwd, m.showEffort, m.showTurnTimer = false, false, false
	m.showGitBranch, m.showTotalTokens = false, false
	if out := m.renderTopBar(); out != "" {
		t.Fatalf("all toggles off should hide default line, got %q", out)
	}
}