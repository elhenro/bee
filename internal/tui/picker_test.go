package tui

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
)

func testCfg() config.Config {
	return config.Config{
		DefaultProvider: "alpha",
		DefaultModel:    "alpha-m1",
		Providers: map[string]config.ProviderConfig{
			"alpha": {BaseURL: "https://alpha.example/v1", WireAPI: "chat", DefaultModel: "alpha-m1"},
			"beta":  {BaseURL: "https://beta.example/v1", WireAPI: "chat", DefaultModel: "beta-m1"},
		},
	}
}

func fakeLister(models map[string][]llm.Model, errs map[string]error) ModelLister {
	return func(_ context.Context, name string, _ config.ProviderConfig) ([]llm.Model, error) {
		if e, ok := errs[name]; ok {
			return nil, e
		}
		return models[name], nil
	}
}

// drainCmd runs a tea.Cmd to completion, returning the resulting message.
func drainCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	return cmd()
}

// settle delivers async messages produced by the load to the picker.
func settle(t *testing.T, p *Picker, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	// shallow drain: run the cmd and feed any returned message back. tea.Batch
	// returns BatchMsg with []Cmd. Handle one level deep — enough for our flows.
	msg := cmd()
	switch m := msg.(type) {
	case tea.BatchMsg:
		for _, c := range m {
			settle(t, p, c)
		}
	case modelsLoadedMsg:
		p.Update(m)
	}
}

func TestPicker_ShowLoadsCurrentProvider(t *testing.T) {
	p := NewPicker(testCfg())
	p.SetLister(fakeLister(map[string][]llm.Model{
		"alpha": {{ID: "alpha-m1", Name: "Alpha M1"}, {ID: "alpha-m2", Name: "Alpha M2"}},
		"beta":  {{ID: "beta-m1", Name: "Beta M1"}},
	}, nil))
	p.SetSize(100, 30)

	settle(t, p, p.Show())

	if !p.Active() {
		t.Fatal("picker not active after Show")
	}
	if got := p.providerSelectedName(); got != "alpha" {
		t.Errorf("default selection = %q, want alpha", got)
	}
	if len(p.modelsByProvider["alpha"]) != 2 {
		t.Errorf("alpha cache len = %d", len(p.modelsByProvider["alpha"]))
	}
}

func TestPicker_ArrowMovesSelection(t *testing.T) {
	p := NewPicker(testCfg())
	p.SetLister(fakeLister(map[string][]llm.Model{
		"alpha": {{ID: "alpha-m1"}},
		"beta":  {{ID: "beta-m1"}},
	}, nil))
	p.SetSize(100, 30)
	settle(t, p, p.Show())

	// down arrow moves to next provider
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyDown})
	settle(t, p, cmd)

	if got := p.providerSelectedName(); got != "beta" {
		t.Errorf("after down, selection = %q, want beta", got)
	}
	// beta only auto-loads when user advances to stage 2 (enter). Trigger that.
	_, cmd = p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	settle(t, p, cmd)
	if len(p.modelsByProvider["beta"]) != 1 {
		t.Errorf("beta models not loaded after advance: %+v", p.modelsByProvider)
	}
}

func TestPicker_EnterEmitsPickedMsg(t *testing.T) {
	p := NewPicker(testCfg())
	p.SetLister(fakeLister(map[string][]llm.Model{
		"alpha": {{ID: "alpha-m1", Name: "Alpha M1"}},
	}, nil))
	p.SetSize(100, 30)
	settle(t, p, p.Show())

	// enter advances to model stage, second enter commits the highlighted model
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	settle(t, p, cmd)

	_, cmd = p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := drainCmd(t, cmd)
	picked, ok := msg.(PickedMsg)
	if !ok {
		t.Fatalf("expected PickedMsg, got %T (%+v)", msg, msg)
	}
	if picked.Provider != "alpha" || picked.Model != "alpha-m1" {
		t.Errorf("picked = %+v", picked)
	}
	if p.Active() {
		t.Error("picker should be inactive after commit")
	}
}

func TestPicker_EscDismisses(t *testing.T) {
	p := NewPicker(testCfg())
	p.SetLister(fakeLister(map[string][]llm.Model{"alpha": {{ID: "m"}}}, nil))
	p.SetSize(100, 30)
	settle(t, p, p.Show())

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	msg := drainCmd(t, cmd)
	if _, ok := msg.(PickerDismissedMsg); !ok {
		t.Fatalf("expected PickerDismissedMsg, got %T", msg)
	}
	if p.Active() {
		t.Error("picker should be inactive after esc")
	}
}

func TestPicker_ErrorAndRetry(t *testing.T) {
	calls := 0
	errs := map[string]error{"alpha": errors.New("boom")}
	lister := func(_ context.Context, name string, _ config.ProviderConfig) ([]llm.Model, error) {
		calls++
		if calls == 1 {
			return nil, errs[name]
		}
		return []llm.Model{{ID: "ok"}}, nil
	}

	p := NewPicker(testCfg())
	p.SetLister(lister)
	p.SetSize(100, 30)
	settle(t, p, p.Show())

	if p.loadErr["alpha"] == nil {
		t.Fatal("expected error to be stored")
	}

	// advance to model stage so ctrl+r refresh is in scope. enter re-triggers
	// loadProvider; drain that so the retry path sees a clean state.
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	settle(t, p, cmd)
	_, cmd = p.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	settle(t, p, cmd)

	if p.loadErr["alpha"] != nil {
		t.Errorf("error should clear on retry success, got %v", p.loadErr["alpha"])
	}
	if len(p.modelsByProvider["alpha"]) != 1 {
		t.Errorf("retry did not populate cache: %+v", p.modelsByProvider)
	}
}
