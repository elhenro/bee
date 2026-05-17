package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sahilm/fuzzy"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
)

// providerItem is a row in the provider stage.
type providerItem struct {
	name string
	cfg  config.ProviderConfig
}

// modelItem is a row in the model stage.
type modelItem struct {
	id      string
	display string
	desc    string
}

func defaultLister(ctx context.Context, name string, cfg config.ProviderConfig) ([]llm.Model, error) {
	return llm.ListModels(ctx, name, cfg)
}

// fuzzyAll runs fuzzy.Find on a pool, or returns pool-order matches when the
// query is empty so the renderer stays a single code path.
func fuzzyAll[T any](query string, pool []T, hayFn func(int) string) []fuzzy.Match {
	q := strings.TrimSpace(query)
	if q == "" {
		out := make([]fuzzy.Match, len(pool))
		for i := range pool {
			out[i] = fuzzy.Match{Index: i, Str: hayFn(i)}
		}
		return out
	}
	hay := make([]string, len(pool))
	for i := range pool {
		hay[i] = hayFn(i)
	}
	return fuzzy.Find(q, hay)
}

// buildProviderList builds a sorted provider slice from the config map.
func buildProviderList(cfg config.Config) []providerItem {
	names := make([]string, 0, len(cfg.Providers))
	for n := range cfg.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]providerItem, 0, len(names))
	for _, n := range names {
		out = append(out, providerItem{name: n, cfg: cfg.Providers[n]})
	}
	return out
}

// modelEntries adapts llm.Model results into picker rows.
func modelEntries(models []llm.Model) []modelItem {
	out := make([]modelItem, 0, len(models))
	for _, m := range models {
		desc := m.ID
		if m.ContextLength > 0 {
			desc = fmt.Sprintf("%s · ctx %d", m.ID, m.ContextLength)
		}
		if m.Pricing != "" {
			desc += " · " + m.Pricing
		}
		display := m.Name
		if display == "" {
			display = m.ID
		}
		out = append(out, modelItem{id: m.ID, display: display, desc: desc})
	}
	return out
}
