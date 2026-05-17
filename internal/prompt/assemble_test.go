package prompt

import (
	"strings"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
)

func baseCfg() config.Config {
	c := config.Defaults()
	c.Caveman = "off" // disable rules text in tests for clean assertions
	return c
}

func TestAssembleSectionOrder(t *testing.T) {
	cfg := baseCfg()
	specs := []llm.ToolSpec{
		{Name: "shell", Description: "run shell"},
		{Name: "read", Description: "read file"},
	}
	skills := "calc: commit (prompt)"
	recs := []knowledge.Record{{
		Entry: knowledge.Entry{Name: "user-pref", Priority: 3, Modified: time.Now()},
		Body:  "prefer pnpm",
	}}

	out := Assemble(cfg, specs, skills, recs, nil)

	want := []string{
		"bee coding agent",
		"## Tools",
		"- shell:",
		"## Skills",
		"calc: commit (prompt)",
		"## Memory",
		"<memory name=\"user-pref\"",
		"prefer pnpm",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("missing section %q in:\n%s", w, out)
		}
	}

	idxTools := strings.Index(out, "## Tools")
	idxSkills := strings.Index(out, "## Skills")
	idxMem := strings.Index(out, "## Memory")
	if !(idxTools < idxSkills && idxSkills < idxMem) {
		t.Errorf("section order wrong: tools=%d skills=%d mem=%d",
			idxTools, idxSkills, idxMem)
	}
}

func TestAssembleStalenessNoteOnExpired(t *testing.T) {
	cfg := baseCfg()
	past := time.Now().Add(-72 * time.Hour)
	recs := []knowledge.Record{{
		Entry: knowledge.Entry{
			Name:      "stale",
			Modified:  past,
			ExpiresAt: past,
			Priority:  3,
		},
		Body: "old fact",
	}}
	out := Assemble(cfg, nil, "", recs, nil)
	if !strings.Contains(out, "expired") {
		t.Errorf("expected staleness note for expired record; got:\n%s", out)
	}
}

func TestAssembleNoStalenessForLive(t *testing.T) {
	cfg := baseCfg()
	recs := []knowledge.Record{{
		Entry: knowledge.Entry{
			Name:      "fresh",
			Modified:  time.Now(),
			ExpiresAt: time.Now().Add(24 * time.Hour),
			Priority:  3,
		},
		Body: "today fact",
	}}
	out := Assemble(cfg, nil, "", recs, nil)
	if strings.Contains(out, "expired") {
		t.Errorf("unexpected staleness note for live record:\n%s", out)
	}
}

func TestAssembleTruncatesToolDesc(t *testing.T) {
	cfg := baseCfg()
	cfg.Profile = "tiny" // ToolDescChars=80
	long := strings.Repeat("x", 500)
	specs := []llm.ToolSpec{{Name: "shell", Description: long}}
	out := Assemble(cfg, specs, "", nil, nil)
	if !strings.Contains(out, "…") {
		t.Errorf("expected truncation ellipsis in:\n%s", out)
	}
	if strings.Contains(out, long) {
		t.Errorf("full long description should not appear")
	}
}

func TestAssembleBudgetDropsMemoriesFirst(t *testing.T) {
	cfg := baseCfg()
	// shrink budget aggressively
	cfg.Profiles["normal"] = config.Profile{
		SystemPromptBudget: 60, // ~240 chars
		ToolDescChars:      40,
	}
	bigBody := strings.Repeat("memory-content ", 50)
	recs := []knowledge.Record{
		{Entry: knowledge.Entry{Name: "a", Modified: time.Now(), Priority: 3}, Body: bigBody},
		{Entry: knowledge.Entry{Name: "b", Modified: time.Now(), Priority: 3}, Body: bigBody},
	}
	skills := "calc: commit (prompt)"
	out := Assemble(cfg, nil, skills, recs, nil)
	if strings.Contains(out, bigBody) {
		t.Errorf("expected records trimmed under tight budget; got:\n%s", out)
	}
}

func TestAssembleEmptySections(t *testing.T) {
	cfg := baseCfg()
	out := Assemble(cfg, nil, "", nil, nil)
	if strings.Contains(out, "## Tools") {
		t.Errorf("empty tools should not render a Tools header")
	}
	if strings.Contains(out, "## Skills") {
		t.Errorf("empty skill manifest should not render a Skills header")
	}
	if strings.Contains(out, "## Memory") {
		t.Errorf("empty records should not render a Memory header")
	}
	if !strings.Contains(out, "bee coding agent") {
		t.Errorf("identity always present")
	}
}

func TestAssembleTruncatesRecordBodyTiny(t *testing.T) {
	cfg := baseCfg()
	cfg.Profile = "tiny" // MemoryBodyChars=400
	body := strings.Repeat("ab ", 667)[:2000] // 2000-char word-aligned body
	recs := []knowledge.Record{{
		Entry: knowledge.Entry{Name: "big", Modified: time.Now(), Priority: 3},
		Body:  body,
	}}
	out := Assemble(cfg, nil, "", recs, nil)

	start := strings.Index(out, "## Memory")
	if start < 0 {
		t.Fatalf("missing Memory section:\n%s", out)
	}
	section := out[start:]
	// body cap 400 + memory-tag frame ≈ 53 bytes + 3-byte ellipsis.
	if len(section) > 480 {
		t.Errorf("memory section should be ≤480 chars after truncation; got %d:\n%s", len(section), section)
	}
	if !strings.Contains(section, "…") {
		t.Errorf("expected truncation ellipsis in body; got:\n%s", section)
	}
	if strings.Contains(section, body) {
		t.Errorf("full body should not appear; section:\n%s", section)
	}
}

func TestAssembleKeepsFullRecordBodyLarge(t *testing.T) {
	cfg := baseCfg()
	cfg.Profile = "large" // MemoryBodyChars=0 → unbounded
	body := strings.Repeat("alpha beta ", 200)
	recs := []knowledge.Record{{
		Entry: knowledge.Entry{Name: "big", Modified: time.Now(), Priority: 3},
		Body:  body,
	}}
	out := Assemble(cfg, nil, "", recs, nil)
	if !strings.Contains(out, strings.TrimSpace(body)) {
		t.Errorf("large profile should keep full body")
	}
}

func TestEstimateTokensRoughly(t *testing.T) {
	if got := EstimateTokens("abcd"); got != 1 {
		t.Errorf("4 chars expected 1 token, got %d", got)
	}
	if got := EstimateTokens(strings.Repeat("x", 400)); got != 100 {
		t.Errorf("400 chars expected 100 tokens, got %d", got)
	}
}
