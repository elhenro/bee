package loop

import (
	"context"
	"testing"

	"github.com/elhenro/bee/internal/ask"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/types"
)

type visionAsker struct {
	ans   ask.Answer
	calls int
}

func (a *visionAsker) Ask(context.Context, ask.Question) (ask.Answer, error) {
	a.calls++
	return a.ans, nil
}

func keepsImage(out []types.Message) bool { return out[0].Content[1].Type == types.BlockImage }

func askEngine(a *visionAsker, saved map[string]bool) *Engine {
	return &Engine{
		Cfg:         config.Config{DefaultModel: "mystery-local:7b"},
		VisionAsker: a,
		PersistVision: func(m string, v bool) error {
			saved[m] = v
			return nil
		},
		run: &runState{},
	}
}

func TestAskVision_YesSendsImageAndIsRemembered(t *testing.T) {
	a, saved := &visionAsker{ans: ask.Answer{Index: 0}}, map[string]bool{}
	e := askEngine(a, saved)
	if !keepsImage(e.applyVisionFallback(context.Background(), imgMsg())) {
		t.Fatal("answered yes: image must reach the model")
	}
	if !keepsImage(e.applyVisionFallback(context.Background(), imgMsg())) {
		t.Fatal("second turn lost the image")
	}
	if a.calls != 1 {
		t.Errorf("asked %d times, want 1", a.calls)
	}
	if v, ok := saved["mystery-local:7b"]; !ok || !v {
		t.Errorf("answer not persisted: %v", saved)
	}
}

func TestAskVision_NoDropsImageAndIsRemembered(t *testing.T) {
	a, saved := &visionAsker{ans: ask.Answer{Index: 1}}, map[string]bool{}
	e := askEngine(a, saved)
	if keepsImage(e.applyVisionFallback(context.Background(), imgMsg())) {
		t.Fatal("answered no: image must not reach the model")
	}
	e.applyVisionFallback(context.Background(), imgMsg())
	if a.calls != 1 {
		t.Errorf("asked %d times, want 1", a.calls)
	}
	if v, ok := saved["mystery-local:7b"]; !ok || v {
		t.Errorf("answer not persisted as false: %v", saved)
	}
}

func TestAskVision_DismissIsNotPersistedNorReasked(t *testing.T) {
	a, saved := &visionAsker{ans: ask.Answer{Index: -1, Dismissed: true}}, map[string]bool{}
	e := askEngine(a, saved)
	for range 3 {
		if keepsImage(e.applyVisionFallback(context.Background(), imgMsg())) {
			t.Fatal("dismissed: image must be treated as unsupported")
		}
	}
	if a.calls != 1 || len(saved) != 0 {
		t.Errorf("calls=%d saved=%v, want 1 call and nothing saved", a.calls, saved)
	}
}

func TestAskVision_NotAskedWhenKnown(t *testing.T) {
	a, saved := &visionAsker{ans: ask.Answer{Index: 1}}, map[string]bool{}
	e := askEngine(a, saved)
	e.Cfg.DefaultModel = "claude-sonnet-4-6"
	if !keepsImage(e.applyVisionFallback(context.Background(), imgMsg())) {
		t.Fatal("known vision model lost its image")
	}
	// the user's own verdict outranks the name list, and is not re-asked
	e.Cfg.Vision.Models = map[string]bool{"claude-sonnet-4-6": false}
	if keepsImage(e.applyVisionFallback(context.Background(), imgMsg())) {
		t.Fatal("user said no: image must be dropped even for a name-list match")
	}
	if a.calls != 0 {
		t.Errorf("asked %d times for known models, want 0", a.calls)
	}
}

func TestAskVision_NoAskerNeverPrompts(t *testing.T) {
	e := askEngine(nil, map[string]bool{})
	e.VisionAsker = nil
	if keepsImage(e.applyVisionFallback(context.Background(), imgMsg())) {
		t.Fatal("headless unknown model must stay blind")
	}
}
