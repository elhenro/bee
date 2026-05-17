package shell

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/approval"
)

func TestApprover_DenyAbortsDangerous(t *testing.T) {
	tool := NewWithApprover(approval.Static{Verdict: approval.Deny})
	res, err := tool.Run(context.Background(), map[string]any{"command": "rm -rf ./tmpx"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("dangerous cmd should be refused")
	}
	if !strings.Contains(res.Content, "refused by user") {
		t.Fatalf("unexpected refusal text: %q", res.Content)
	}
}

func TestApprover_AllowRuns(t *testing.T) {
	tool := NewWithApprover(approval.Static{Verdict: approval.AllowOnce})
	// recursive delete on a path that doesn't exist — checks approver passes,
	// not that rm succeeds; rm reports its own error which is fine.
	res, err := tool.Run(context.Background(), map[string]any{"command": "rm -rf /tmp/bee-approver-nonexistent-xyz"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError && strings.Contains(res.Content, "refused by user") {
		t.Fatalf("approver allowed but refusal returned: %q", res.Content)
	}
}

func TestApprover_NotConsultedForSafeCmd(t *testing.T) {
	// Static{Deny} would refuse if asked, but a safe cmd should never reach it.
	tool := NewWithApprover(approval.Static{Verdict: approval.Deny})
	res, err := tool.Run(context.Background(), map[string]any{"command": "echo ok"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("safe cmd refused: %q", res.Content)
	}
	if !strings.Contains(res.Content, "ok") {
		t.Fatalf("missing output: %q", res.Content)
	}
}

func TestApprover_HardlineStillBlocks(t *testing.T) {
	// Even AllowAlways doesn't bypass safety.CheckShellCommand hardline.
	tool := NewWithApprover(approval.Static{Verdict: approval.AllowAlways})
	res, err := tool.Run(context.Background(), map[string]any{"command": "rm -rf /"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Content, "refused") {
		t.Fatalf("hardline should refuse regardless of approver: %q", res.Content)
	}
}

// captureApprover records the cmd string handed to Request, so we can verify
// the modal sees the unwrapped command — not the sandbox-exec helper blob.
type captureApprover struct {
	verdict approval.Decision
	gotCmd  string
	gotKey  string
	gotDesc string
}

func (c *captureApprover) Request(_ context.Context, cmd, key, desc string) (approval.Decision, error) {
	c.gotCmd, c.gotKey, c.gotDesc = cmd, key, desc
	return c.verdict, nil
}

func TestApprover_PrefersOrigCommandForDisplay(t *testing.T) {
	cap := &captureApprover{verdict: approval.Deny}
	tool := NewWithApprover(cap)
	wrapped := "sandbox-exec -p '(version 1)...' bash -c 'rm -rf ./tmpx'"
	_, err := tool.Run(context.Background(), map[string]any{
		"command":        wrapped,
		"_orig_command": "rm -rf ./tmpx",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.gotCmd != "rm -rf ./tmpx" {
		t.Errorf("approver.Request cmd = %q, want unwrapped %q", cap.gotCmd, "rm -rf ./tmpx")
	}
	if !strings.Contains(cap.gotKey, "rm") {
		t.Errorf("danger key %q does not look like rm-pattern", cap.gotKey)
	}
}

func TestApprover_Nil_NoGating(t *testing.T) {
	tool := New() // no approver
	res, err := tool.Run(context.Background(), map[string]any{"command": "rm -rf /tmp/bee-no-approver-nonexistent-xyz"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError && strings.Contains(res.Content, "refused by user") {
		t.Fatalf("nil approver should not gate: %q", res.Content)
	}
}
