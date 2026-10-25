package dirgate

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

type counting struct {
	d approval.Decision
	n int
}

func (c *counting) Request(context.Context, string, string, string) (approval.Decision, error) {
	c.n++
	return c.d, nil
}

func TestInsideNeverAsks(t *testing.T) {
	root := t.TempDir()
	a := &counting{d: approval.Deny}
	g := New(root, a, nil)
	if msg := g.Check(context.Background(), filepath.Join(root, "x.go")); msg != "" || a.n != 0 {
		t.Fatalf("msg=%q asks=%d", msg, a.n)
	}
}

func TestOutsideDeny(t *testing.T) {
	a := &counting{d: approval.Deny}
	g := New(t.TempDir(), a, nil)
	if msg := g.Check(context.Background(), filepath.Join(t.TempDir(), "x")); msg == "" {
		t.Fatal("want denial")
	}
}

func TestSessionGrantCoversSubtree(t *testing.T) {
	other := t.TempDir()
	a := &counting{d: approval.AllowSession}
	g := New(t.TempDir(), a, nil)
	ctx := context.Background()
	for _, p := range []string{filepath.Join(other, "a"), filepath.Join(other, "b", "c")} {
		if msg := g.Check(ctx, p); msg != "" {
			t.Fatal(msg)
		}
	}
	if a.n != 1 {
		t.Fatalf("asks=%d, want 1", a.n)
	}
}

func TestAllowOnceAsksAgain(t *testing.T) {
	other := t.TempDir()
	a := &counting{d: approval.AllowOnce}
	g := New(t.TempDir(), a, nil)
	g.Check(context.Background(), filepath.Join(other, "a"))
	g.Check(context.Background(), filepath.Join(other, "a"))
	if a.n != 2 {
		t.Fatalf("asks=%d, want 2", a.n)
	}
}

func TestSeedFromAllowlist(t *testing.T) {
	other := t.TempDir()
	a := &counting{d: approval.Deny}
	g := New(t.TempDir(), a, []string{"rm-recursive", KeyPrefix + other})
	if msg := g.Check(context.Background(), filepath.Join(other, "f")); msg != "" || a.n != 0 {
		t.Fatalf("msg=%q asks=%d", msg, a.n)
	}
}

type pathEcho struct{ got string }

func (p *pathEcho) Spec() llm.ToolSpec { return llm.ToolSpec{Name: "echo"} }
func (p *pathEcho) Run(_ context.Context, in map[string]any) (tools.Result, error) {
	p.got, _ = in["path"].(string)
	return tools.Result{}, nil
}

func TestWrapExpandsHomeForInnerTool(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads this one on windows
	inner := &pathEcho{}
	g := New(home, approval.Static{Verdict: approval.AllowOnce}, nil)
	if _, err := g.Wrap(inner).Run(context.Background(), map[string]any{"path": "~/a.md"}); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "a.md"); inner.got != want {
		t.Fatalf("inner saw %q, want %q", inner.got, want)
	}
}
