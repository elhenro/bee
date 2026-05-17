package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func strip(s string) string { return ansi.Strip(s) }

func TestHive_RenderStrip_Empty(t *testing.T) {
	h := &Hive{}
	out := strip(h.Render(80))
	if !strings.Contains(out, "no bees") {
		t.Fatalf("want 'no bees' placeholder, got %q", out)
	}
}

func TestHive_RenderStrip_OrdersActiveFirst(t *testing.T) {
	h := &Hive{maxStrip: 8}
	h.Set([]Bee{
		{Name: "doc", State: Idle, StartedAt: time.Now()},
		{Name: "main", State: Active, StartedAt: time.Now()},
		{Name: "test", State: Awaiting, StartedAt: time.Now()},
	})
	out := strip(h.Render(80))
	if !strings.Contains(out, "main") {
		t.Fatalf("missing main bee: %q", out)
	}
	mIdx := strings.Index(out, "main")
	dIdx := strings.Index(out, "doc")
	if mIdx == -1 || dIdx == -1 || mIdx > dIdx {
		t.Fatalf("active bee should precede idle; got %q", out)
	}
	if !strings.Contains(out, "⬢") {
		t.Fatalf("filled hex glyph missing: %q", out)
	}
	if !strings.Contains(out, "⬡") {
		t.Fatalf("hollow hex glyph missing: %q", out)
	}
}

func TestHive_Toggle(t *testing.T) {
	h := &Hive{}
	if h.Expanded() {
		t.Fatal("should start collapsed")
	}
	h.Update(ToggleHiveMsg{})
	if !h.Expanded() {
		t.Fatal("toggle should expand")
	}
	h.Update(ToggleHiveMsg{})
	if h.Expanded() {
		t.Fatal("toggle should collapse")
	}
}

func TestHive_RenderFull_ContainsTitleAndSections(t *testing.T) {
	h := &Hive{}
	h.Set([]Bee{
		{Name: "main", State: Active, Model: "gpt-test"},
		{Name: "doc", State: Done, SessionID: "abc12345"},
	})
	out := strip(h.RenderFull(80, 24))
	if !strings.Contains(out, "Hive") {
		t.Fatalf("missing title: %q", out)
	}
	if !strings.Contains(out, "main") {
		t.Fatalf("missing active bee in grid: %q", out)
	}
	if !strings.Contains(out, "recent sessions") {
		t.Fatalf("missing recent sessions header: %q", out)
	}
	if !strings.Contains(out, "abc12345") {
		t.Fatalf("missing session id in recent list: %q", out)
	}
}

func TestHive_Upsert(t *testing.T) {
	h := &Hive{}
	h.Upsert(Bee{Name: "main", State: Idle, SessionID: "s1"})
	h.Upsert(Bee{Name: "main", State: Active, SessionID: "s1"})
	if got := h.Bees(); len(got) != 1 || got[0].State != Active {
		t.Fatalf("upsert should replace; got %+v", got)
	}
	h.Upsert(Bee{Name: "test", State: Idle, SessionID: "s2"})
	if got := h.Bees(); len(got) != 2 {
		t.Fatalf("upsert should append new bee; got %d", len(got))
	}
}

func TestHexRow_TruncatesToWidth(t *testing.T) {
	states := []BeeState{Active, Active, Active, Active, Active, Active}
	names := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta"}
	out := HexRow(states, names, 20)
	if w := len([]rune(strip(out))); w > 20 {
		t.Fatalf("truncate failed: width=%d output=%q", w, strip(out))
	}
}

func TestHexRow_Empty(t *testing.T) {
	out := strip(HexRow(nil, nil, 80))
	if !strings.Contains(out, "no bees") {
		t.Fatalf("want placeholder, got %q", out)
	}
}

func TestNewHive_DoesNotPanic(t *testing.T) {
	h := NewHive()
	if h == nil {
		t.Fatal("NewHive returned nil")
	}
	// May or may not have bees depending on ~/.bee/sessions contents; just
	// assert Render is well-formed.
	if got := h.Render(80); got == "" {
		t.Fatal("Render returned empty")
	}
}
