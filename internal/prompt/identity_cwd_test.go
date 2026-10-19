package prompt

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/config"
)

// a worker rooted in its own worktree must be told that directory, not the
// process cwd.
func TestAssembleWithDynamicNamesGivenCwd(t *testing.T) {
	wt := t.TempDir()
	cfg := config.Defaults()
	got, _ := AssembleWithDynamic(cfg, wt, nil, "", nil, nil)
	if !strings.Contains(got, wt) {
		t.Fatalf("prompt does not name the worktree %q:\n%s", wt, got)
	}
	if proc := getCwd(); proc != wt && strings.Contains(got, "cwd: "+proc+".") {
		t.Fatalf("prompt still names the process cwd %q", proc)
	}
}
