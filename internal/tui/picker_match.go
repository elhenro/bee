package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"
)

func (p *Picker) selectedProvider() string {
	matches := p.matchProviders()
	if p.provSel < 0 || p.provSel >= len(matches) {
		return ""
	}
	return p.providers[matches[p.provSel].Index].name
}

func (p *Picker) selectedModelID() string {
	matches := p.matchModels()
	if p.modelSel < 0 || p.modelSel >= len(matches) {
		return ""
	}
	return p.modelsByProvider[p.currentProvider][matches[p.modelSel].Index].id
}

// loadCurrentProvider kicks off a fetch for whichever provider is highlighted.
func (p *Picker) loadCurrentProvider() tea.Cmd {
	name := p.selectedProvider()
	if name == "" {
		return nil
	}
	p.currentProvider = name
	return p.loadProvider(name)
}

// loadProvider starts an async fetch unless we already have it cached.
func (p *Picker) loadProvider(name string) tea.Cmd {
	if _, ok := p.modelsByProvider[name]; ok {
		return nil
	}
	if p.loading[name] {
		return p.spin.Tick
	}
	p.loading[name] = true
	cfg := p.cfg.Providers[name]
	lister := p.lister
	return tea.Batch(
		p.spin.Tick,
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			models, err := lister(ctx, name, cfg)
			return modelsLoadedMsg{provider: name, models: models, err: err}
		},
	)
}

// activeMatches returns the current column's fuzzy-filtered match slice.
func (p *Picker) activeMatches() []fuzzy.Match {
	if p.focus == colProviders {
		return p.matchProviders()
	}
	return p.matchModels()
}

func (p *Picker) matchProviders() []fuzzy.Match {
	return fuzzyAll(p.provQuery, p.providers, func(i int) string {
		return p.providers[i].name + " " + p.providers[i].cfg.BaseURL
	})
}

func (p *Picker) matchModels() []fuzzy.Match {
	models := p.modelsByProvider[p.currentProvider]
	return fuzzyAll(p.modelQuery, models, func(i int) string {
		return models[i].display + " " + models[i].id + " " + models[i].desc
	})
}

// helpers (fuzzyAll, buildProviderList, modelEntries) live in picker_items.go.
