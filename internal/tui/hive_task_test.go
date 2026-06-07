package tui

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func textMsg(role types.Role, s string) types.Message {
	return types.Message{Role: role, Content: []types.ContentBlock{{Type: types.BlockText, Text: s}}}
}

func TestHiveTaskWithContext_PlainTaskUnchanged(t *testing.T) {
	content := []types.ContentBlock{{Type: types.BlockText, Text: "add a forest biome"}}
	got := hiveTaskWithContext(content, nil)
	if got != "add a forest biome" {
		t.Fatalf("plain task altered: %q", got)
	}
}

func TestHiveTaskWithContext_ContinuationReanchors(t *testing.T) {
	history := []types.Message{
		textMsg(types.RoleUser, "improve the game world: add grass, forest, playground biomes"),
		textMsg(types.RoleAssistant, "added Grass.ts and Forest.ts; Playground.ts still pending"),
	}
	content := []types.ContentBlock{{Type: types.BlockText, Text: "continue"}}
	got := hiveTaskWithContext(content, history)

	if !strings.Contains(got, "add grass, forest, playground biomes") {
		t.Fatalf("anchor task missing: %q", got)
	}
	if !strings.Contains(got, "Playground.ts still pending") {
		t.Fatalf("progress missing: %q", got)
	}
	if got == "continue" {
		t.Fatal("continuation reached planner verbatim")
	}
}

func TestHiveTaskWithContext_ContinuationNoHistoryFallsBack(t *testing.T) {
	content := []types.ContentBlock{{Type: types.BlockText, Text: "continue"}}
	if got := hiveTaskWithContext(content, nil); got != "continue" {
		t.Fatalf("want literal fallback, got %q", got)
	}
}

func TestResolveTaskFromHistory_SkipsEphemeralAndContinuations(t *testing.T) {
	history := []types.Message{
		textMsg(types.RoleUser, "build the thing"),
		textMsg(types.RoleAssistant, "did part one"),
		textMsg(types.RoleUser, "continue"),
		{Role: types.RoleUser, Ephemeral: true, Content: []types.ContentBlock{{Type: types.BlockText, Text: "(/new done)"}}},
		textMsg(types.RoleAssistant, "did part two"),
	}
	anchor, progress := resolveTaskFromHistory(history)
	if anchor != "build the thing" {
		t.Fatalf("anchor = %q", anchor)
	}
	if progress != "did part two" {
		t.Fatalf("progress = %q", progress)
	}
}

func TestIsContinuation(t *testing.T) {
	for _, s := range []string{"continue", "Continue.", " go on ", "keep going", "proceed", "yes"} {
		if !isContinuation(s) {
			t.Errorf("expected continuation: %q", s)
		}
	}
	for _, s := range []string{"add a biome", "continue the forest work with trees", ""} {
		if s != "" && isContinuation(s) {
			t.Errorf("unexpected continuation: %q", s)
		}
	}
}
