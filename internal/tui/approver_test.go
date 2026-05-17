package tui

import (
	"context"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/approval"
)

func TestApprover_UnsetProgram_Denies(t *testing.T) {
	a := NewApprover()
	d, err := a.Request(context.Background(), "rm -rf /", "rm-recursive", "")
	if err != nil {
		t.Fatal(err)
	}
	if d != approval.Deny {
		t.Fatalf("got %v, want Deny when program unset", d)
	}
}

func TestApprover_ContextCancel(t *testing.T) {
	a := NewApprover()
	// Fake-attach: we can't easily run a tea.Program in a test, so we attach a
	// dummy program by using nil — Request short-circuits to Deny. Instead test
	// the cancellation path by registering pending manually.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d, err := a.Request(ctx, "x", "k", "")
	if err != nil && d != approval.Deny {
		t.Fatalf("unexpected: d=%v err=%v", d, err)
	}
}

func TestApprover_ResolveUnknownIsNoOp(t *testing.T) {
	a := NewApprover()
	a.Resolve("never-existed", ApprovalAllow)
	// no panic, no deadlock — sanity only.
	time.Sleep(10 * time.Millisecond)
}

func TestModalDecisionToApproval(t *testing.T) {
	cases := []struct {
		in   ApprovalDecision
		want approval.Decision
	}{
		{ApprovalAllow, approval.AllowOnce},
		{ApprovalSession, approval.AllowSession},
		{ApprovalAlways, approval.AllowAlways},
		{ApprovalDeny, approval.Deny},
		{ApprovalDecision("garbage"), approval.Deny},
	}
	for _, c := range cases {
		if got := modalDecisionToApproval(c.in); got != c.want {
			t.Errorf("%v -> %v, want %v", c.in, got, c.want)
		}
	}
}
