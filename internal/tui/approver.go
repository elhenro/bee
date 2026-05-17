package tui

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/approval"
)

// Approver implements approval.Approver by routing requests through the TUI.
//
// Call site lives in shell.Tool (or any other tool) which runs on the engine
// goroutine. Request sends an ApprovalAskMsg to the bubbletea program, then
// blocks on a per-request reply channel. The Model handles the msg by Show()ing
// the modal; once the user picks, the resulting ApprovalDecisionMsg is fed
// back through Resolve which unblocks the caller.
//
// Construct with NewApprover; call SetProgram after tea.NewProgram returns its
// handle. Until SetProgram is called every Request returns Deny so the engine
// can't accidentally race the TUI startup.
type Approver struct {
	program *tea.Program

	mu      sync.Mutex
	nextID  atomic.Uint64
	pending map[string]chan approval.Decision
}

// NewApprover returns an unwired approver. Call SetProgram to attach the tea
// program once tea.NewProgram has returned.
func NewApprover() *Approver {
	return &Approver{pending: map[string]chan approval.Decision{}}
}

// SetProgram wires the tea.Program handle. Safe to call before or after the
// program is running.
func (a *Approver) SetProgram(p *tea.Program) {
	a.mu.Lock()
	a.program = p
	a.mu.Unlock()
}

// Request blocks until the user picks a decision in the TUI modal. Returns
// Deny if the program is unset or the context is cancelled.
func (a *Approver) Request(ctx context.Context, cmd, key, desc string) (approval.Decision, error) {
	a.mu.Lock()
	p := a.program
	a.mu.Unlock()
	if p == nil {
		return approval.Deny, nil
	}

	id := fmt.Sprintf("appr-%d", a.nextID.Add(1))
	ch := make(chan approval.Decision, 1)
	a.mu.Lock()
	a.pending[id] = ch
	a.mu.Unlock()

	p.Send(ApprovalAskMsg{
		UseID:  id,
		Cmd:    cmd,
		Key:    key,
		Reason: desc,
	})

	select {
	case d := <-ch:
		return d, nil
	case <-ctx.Done():
		a.mu.Lock()
		delete(a.pending, id)
		a.mu.Unlock()
		return approval.Deny, ctx.Err()
	}
}

// Resolve delivers a user decision back to the blocked Request call. Called by
// the Model when an ApprovalDecisionMsg arrives. Unknown ids are dropped.
func (a *Approver) Resolve(useID string, d ApprovalDecision) {
	a.mu.Lock()
	ch, ok := a.pending[useID]
	if ok {
		delete(a.pending, useID)
	}
	a.mu.Unlock()
	if ok {
		ch <- modalDecisionToApproval(d)
	}
}

func modalDecisionToApproval(d ApprovalDecision) approval.Decision {
	switch d {
	case ApprovalAllow:
		return approval.AllowOnce
	case ApprovalSession:
		return approval.AllowSession
	case ApprovalAlways:
		return approval.AllowAlways
	default:
		return approval.Deny
	}
}

// ApprovalAskMsg asks the TUI to surface the modal for a flagged command.
type ApprovalAskMsg struct {
	UseID  string
	Cmd    string
	Key    string
	Reason string
}
