package dirgate

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/elhenro/bee/internal/approval"
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
