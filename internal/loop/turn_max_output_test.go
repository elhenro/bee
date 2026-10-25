package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/tools"
)

func TestRun_MaxTokensCapOnLocalProviders(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		set      int
		want     int
	}{
		{"local default", "omlx", 0, config.LocalMaxOutputTokens},
		{"local explicit", "omlx", 4096, 4096},
		{"local lifted", "omlx", -1, 0},
		{"hosted untouched", "anthropic", 0, 0},
		{"hosted explicit", "anthropic", 8000, 8000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &sysCaptureProvider{}
			cfg := config.Defaults()
			cfg.Caveman = "off"
			cfg.Role = "worker"
			cfg.DefaultProvider = tc.provider
			cfg.MaxOutputTokens = tc.set
			cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
			eng := &Engine{Provider: p, Tools: tools.NewRegistry(), Cfg: cfg, Cwd: ".", Stdout: &strings.Builder{}}
			if _, err := eng.Run(context.Background(), "hi"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(p.seen) != 1 {
				t.Fatalf("calls = %d", len(p.seen))
			}
			if got := p.seen[0].MaxTokens; got != tc.want {
				t.Errorf("MaxTokens = %d, want %d", got, tc.want)
			}
		})
	}
}
