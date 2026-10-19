package agents

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
)

// the picker's model fetch result is an unexported message; the overview must
// still hand it to the picker or it stays on "loading" forever.
func TestOverviewPicker_ReceivesFetchedModels(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	m := newModel("/tmp")
	m.picker.SetLister(func(context.Context, string, config.ProviderConfig) ([]llm.Model, error) {
		return []llm.Model{{ID: "model-x"}}, nil
	})
	m.picker.SetSize(80, 24)

	var pump func(c tea.Cmd)
	pump = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case nil:
		case tea.BatchMsg:
			for _, cc := range msg {
				pump(cc)
			}
		default:
			nm, _ := m.Update(msg)
			m = nm.(model)
		}
	}
	pump(m.picker.Show())
	nm, c := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(model)
	pump(c)

	if v := ansi.Strip(m.picker.View()); !strings.Contains(v, "model-x") {
		t.Fatalf("picker never received its models:\n%s", v)
	}
}
