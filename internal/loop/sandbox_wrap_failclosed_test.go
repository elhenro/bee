package loop

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/tools/shell"
)

// a scope that cannot be applied must not run the command bare: the file
// tools treat it as confined, the shell has to agree. checked through the real
// shell tool so the refusal is seen as the model would see it.
func TestWrapShellInput_FailsClosedOnUnapplicableScope(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("policy errors come from the darwin and linux wrappers")
	}
	marker := filepath.Join(t.TempDir(), "ran")
	input := map[string]any{"command": "touch " + marker}
	for name, tc := range map[string]struct {
		scope, cwd string
	}{
		"unknown scope": {"workspace-wrte", "/tmp"},
		"missing cwd":   {"workspace-write", ""},
	} {
		t.Run(name, func(t *testing.T) {
			got := wrapShellInput(input, config.SandboxConfig{Scope: tc.scope}, tc.cwd)
			if got["command"] == input["command"] {
				t.Fatalf("command passed through unwrapped: %v", got["command"])
			}
			res, _ := shell.NewWithApprover(nil).Run(context.Background(), got)
			if !res.IsError || !strings.Contains(res.Content, "exit 126") {
				t.Errorf("expected refusal (exit 126), got %+v", res)
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("command ran despite the refusal")
			}
		})
	}
}
