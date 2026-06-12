package loop

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func thinkTurn(text string) []types.ContentBlock {
	return []types.ContentBlock{{Type: types.BlockThinking, Text: text}}
}

// repeating the same multi-line reasoning across turns must cross the dup
// streak; a divergent turn resets it.
func TestObserveReasoningDup_FiresOnRepeatResetsOnDiverge(t *testing.T) {
	e := &Engine{run: &runState{}}
	reason := "The blocked function checks all colliders in the world.\n" +
		"Let me verify the door gap math against the player radius once more.\n" +
		"So the door gap should work correctly given the clearance we computed.\n"
	// turn 0 seeds the fingerprint, no streak yet.
	if observeReasoningDup(e, thinkTurn(reason)) {
		t.Fatal("first turn should not trip")
	}
	// turns 1..3 repeat it. bail threshold is reasoningDupBailAt consecutive dups.
	fired := false
	for i := 0; i < reasoningDupBailAt; i++ {
		if observeReasoningDup(e, thinkTurn(reason)) {
			fired = true
		}
	}
	if !fired {
		t.Fatalf("expected dup streak to cross %d, streak=%d", reasoningDupBailAt, e.run.reasoningDupStreak)
	}
	// a genuinely different turn resets.
	diff := "Now I will edit Buildings.go to drop the collider on door segments.\n" +
		"That removes the invisible wall at the doorway opening for good.\n" +
		"Then I will rebuild and confirm the player can step inside the house.\n"
	observeReasoningDup(e, thinkTurn(diff))
	if e.run.reasoningDupStreak != 0 {
		t.Fatalf("divergent turn must reset streak, got %d", e.run.reasoningDupStreak)
	}
}

func TestObserveReasoningDup_ThinTurnsDoNotTrip(t *testing.T) {
	e := &Engine{run: &runState{}}
	for i := 0; i < 10; i++ {
		if observeReasoningDup(e, thinkTurn("ok.")) {
			t.Fatal("thin turns must never trip the loop guard")
		}
	}
}

func TestJaccard(t *testing.T) {
	a := map[string]struct{}{"x": {}, "y": {}, "z": {}}
	b := map[string]struct{}{"x": {}, "y": {}, "w": {}}
	if got := jaccard(a, b); got < 0.49 || got > 0.51 {
		t.Fatalf("expected ~0.5, got %v", got)
	}
	if got := jaccard(a, map[string]struct{}{}); got != 0 {
		t.Fatalf("empty set must be 0, got %v", got)
	}
}

func TestReasoningFingerprint_SkipsShortLines(t *testing.T) {
	c := []types.ContentBlock{{Type: types.BlockText, Text: "ok\nyes\n" + strings.Repeat("x", blockMinLineLen) + "\n"}}
	sig := reasoningFingerprint(c)
	// only one substantial line — under reasoningDupMinLines, so nil.
	if sig != nil {
		t.Fatalf("expected nil for thin fingerprint, got %v", sig)
	}
}
