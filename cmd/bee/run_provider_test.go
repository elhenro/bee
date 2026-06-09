package main

import (
	"testing"

	"github.com/elhenro/bee/internal/config"
)

// "" auto-resolves to json on local providers and native (empty) on hosted;
// explicit values always win.
func TestResolveToolFormat(t *testing.T) {
	mk := func(provider, format string) config.Config {
		cfg := config.Defaults()
		cfg.DefaultProvider = provider
		cfg.Profile = "tiny"
		p := cfg.Profiles["tiny"]
		p.ToolFormat = format
		cfg.Profiles["tiny"] = p
		return cfg
	}
	cases := []struct {
		name, provider, format, want string
	}{
		{"local auto", "omlx", "", "json"},
		{"local auto ollama", "ollama", "", "json"},
		{"hosted auto stays native", "openrouter", "", ""},
		{"explicit native wins on local", "omlx", "native", "native"},
		{"explicit xml wins on local", "ollama", "xml", "xml"},
		{"explicit json on hosted", "openrouter", "json", "json"},
	}
	for _, c := range cases {
		if got := resolveToolFormat(mk(c.provider, c.format)); got != c.want {
			t.Errorf("%s: want %q, got %q", c.name, c.want, got)
		}
	}
}
