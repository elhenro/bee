// Package dirgate keeps path-taking file tools inside the directory bee was
// started in. A path outside it asks the user once: allow this call, allow the
// directory for the session, or allow it forever for every agent (persisted by
// the approval cache under the "dir:<path>" key in sandbox.command_allowlist).
package dirgate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

// KeyPrefix marks a directory grant inside the shared approval allowlist.
const KeyPrefix = "dir:"

// Gate decides whether a tool may touch a path outside root.
type Gate struct {
	root string
	app  approval.Approver
	mu   sync.Mutex
	dirs []string // granted directories; a grant covers its subtree
}

// New builds a gate rooted at cwd. seed is the persisted allowlist; entries
// without KeyPrefix are other approval keys and are ignored.
func New(cwd string, app approval.Approver, seed []string) *Gate {
	g := &Gate{root: cwd, app: app}
	for _, k := range seed {
		if d, ok := strings.CutPrefix(k, KeyPrefix); ok {
			g.dirs = append(g.dirs, d)
		}
	}
	return g
}

func (g *Gate) granted(dir string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, d := range g.dirs {
		if dir == d || strings.HasPrefix(dir, strings.TrimSuffix(d, "/")+"/") {
			return true
		}
	}
	return false
}

// Check returns "" when path may be used, else a message for the model.
func (g *Gate) Check(ctx context.Context, path string) string {
	abs, _, _, inside := tools.ResolveInRoot(g.root, path)
	if inside {
		return ""
	}
	dir := abs
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		dir = filepath.Dir(abs)
	}
	if g.granted(dir) {
		return ""
	}
	desc := fmt.Sprintf("path outside working directory %s", g.root)
	d, err := g.app.Request(ctx, abs, KeyPrefix+dir, desc)
	if err != nil || d == approval.Deny {
		return fmt.Sprintf("access to %q denied by user: it is outside the working directory %s", abs, g.root)
	}
	if d != approval.AllowOnce {
		g.mu.Lock()
		g.dirs = append(g.dirs, dir)
		g.mu.Unlock()
	}
	return ""
}

type gated struct {
	tools.Tool
	g *Gate
}

// Wrap returns t guarded by the gate. Tools that take no path pass through the
// "path" lookup untouched, and an empty path means the working directory.
func (g *Gate) Wrap(t tools.Tool) tools.Tool { return gated{t, g} }

func (w gated) Spec() llm.ToolSpec { return w.Tool.Spec() }

func (w gated) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	if p, _ := in["path"].(string); p != "" {
		if msg := w.g.Check(ctx, p); msg != "" {
			return tools.Result{Content: msg, IsError: true}, nil
		}
		// inner tools stat the path as given, so hand them the expanded form
		if ex := tools.ExpandHome(p); ex != p {
			cp := make(map[string]any, len(in))
			for k, v := range in {
				cp[k] = v
			}
			cp["path"] = ex
			in = cp
		}
	}
	return w.Tool.Run(ctx, in)
}
