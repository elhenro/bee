package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/tools"
)

// the system prompt names the engine's Cwd (a worker's worktree), not the
// process cwd.
func TestRun_SystemPromptNamesEngineCwd(t *testing.T) {
	wt := t.TempDir()
	p := &captureProvider{}
	eng, _ := newEngine(p, tools.NewRegistry())
	eng.Cwd = wt
	if _, err := eng.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if sys := p.reqs[0].System; !strings.Contains(sys, wt) {
		t.Fatalf("system prompt does not name %q:\n%s", wt, sys)
	}
}
