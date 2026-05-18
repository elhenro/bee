package agents

import (
	"os"

	"github.com/pelletier/go-toml/v2"

	"github.com/elhenro/bee/internal/config"
)

// Prefs are the agents-overview render + default-spawn toggles. Persisted as
// top-level keys in ~/.bee/config.toml so the file stays flat (one source of
// truth). Defaults preserve the existing look — every Show* is true.
type Prefs struct {
	ShowPeek        bool   `toml:"agents_show_peek"`
	ShowBadges      bool   `toml:"agents_show_badges"`
	ShowChip        bool   `toml:"agents_show_chip"`
	ShowSubheader   bool   `toml:"agents_show_subheader"`
	ShowHint        bool   `toml:"agents_show_hint"`
	ShowMerged      bool   `toml:"agents_show_merged"`
	DefaultModel    string `toml:"agents_default_model"`
	DefaultProvider string `toml:"agents_default_provider"`
}

// DefaultPrefs returns the visible-by-default preset. Used when no config
// file exists or none of the keys are set.
func DefaultPrefs() Prefs {
	return Prefs{
		ShowPeek:      true,
		ShowBadges:    true,
		ShowChip:      true,
		ShowSubheader: true,
		ShowHint:      true,
		ShowMerged:    true,
	}
}

// LoadPrefs reads agents prefs from the bee config file. Missing file or
// missing keys fall back to DefaultPrefs so the user gets a sensible view on
// first launch. Read errors are swallowed — config drift shouldn't break the
// overview from opening.
func LoadPrefs() Prefs {
	p := DefaultPrefs()
	path := config.ConfigPath()
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return p
	}
	// raw map first so missing keys keep their default true values (toml
	// unmarshal would zero them, flipping the defaults).
	var root map[string]any
	if err := toml.Unmarshal(data, &root); err != nil {
		return p
	}
	if v, ok := root["agents_show_peek"].(bool); ok {
		p.ShowPeek = v
	}
	if v, ok := root["agents_show_badges"].(bool); ok {
		p.ShowBadges = v
	}
	if v, ok := root["agents_show_chip"].(bool); ok {
		p.ShowChip = v
	}
	if v, ok := root["agents_show_subheader"].(bool); ok {
		p.ShowSubheader = v
	}
	if v, ok := root["agents_show_hint"].(bool); ok {
		p.ShowHint = v
	}
	if v, ok := root["agents_show_merged"].(bool); ok {
		p.ShowMerged = v
	}
	if v, ok := root["agents_default_model"].(string); ok {
		p.DefaultModel = v
	}
	if v, ok := root["agents_default_provider"].(string); ok {
		p.DefaultProvider = v
	}
	return p
}
