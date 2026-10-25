package tui

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/approval"
)

// broker parks an engine-goroutine caller until the TUI answers its request:
// await sends the request message to the bubbletea program, then blocks on a
// per-request reply channel that resolve fills. Call SetProgram after
// tea.NewProgram returns its handle.
type broker[T any] struct {
	prefix  string // request-id prefix
	program *tea.Program

	mu      sync.Mutex
	nextID  atomic.Uint64
	pending map[string]chan T
}

func newBroker[T any](prefix string) *broker[T] {
	return &broker[T]{prefix: prefix, pending: map[string]chan T{}}
}

// SetProgram wires the tea.Program handle. Safe to call before or after the
// program is running.
func (b *broker[T]) SetProgram(p *tea.Program) {
	b.mu.Lock()
	b.program = p
	b.mu.Unlock()
}

// await reports attached=false when no program is wired. On context
// cancellation it returns cancelled and the context error.
func (b *broker[T]) await(ctx context.Context, request func(id string) tea.Msg, cancelled T) (ans T, attached bool, err error) {
	b.mu.Lock()
	p := b.program
	b.mu.Unlock()
	if p == nil {
		return ans, false, nil
	}

	id := fmt.Sprintf("%s-%d", b.prefix, b.nextID.Add(1))
	ch := make(chan T, 1)
	b.mu.Lock()
	b.pending[id] = ch
	b.mu.Unlock()

	p.Send(request(id))

	select {
	case ans := <-ch:
		return ans, true, nil
	case <-ctx.Done():
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return cancelled, true, ctx.Err()
	}
}

// resolve delivers an answer back to the blocked await. Unknown ids are dropped.
func (b *broker[T]) resolve(id string, ans T) {
	b.mu.Lock()
	ch, ok := b.pending[id]
	delete(b.pending, id)
	b.mu.Unlock()
	if ok {
		ch <- ans
	}
}

// Approver implements approval.Approver by routing requests through the TUI.
//
// Call site lives in shell.Tool (or any other tool) which runs on the engine
// goroutine. Request sends an ApprovalAskMsg to the bubbletea program, then
// blocks until the Model Show()s the modal and the user picks; the resulting
// ApprovalDecisionMsg is fed back through Resolve which unblocks the caller.
//
// Until SetProgram is called every Request returns Deny so the engine can't
// accidentally race the TUI startup.
type Approver struct {
	*broker[approval.Decision]
}

// NewApprover returns an unwired approver. Call SetProgram to attach the tea
// program once tea.NewProgram has returned.
func NewApprover() *Approver {
	return &Approver{newBroker[approval.Decision]("appr")}
}

// Request blocks until the user picks a decision in the TUI modal. Returns
// Deny if the program is unset or the context is cancelled.
func (a *Approver) Request(ctx context.Context, cmd, key, desc string) (approval.Decision, error) {
	d, attached, err := a.await(ctx, func(id string) tea.Msg {
		return ApprovalAskMsg{UseID: id, Cmd: cmd, Key: key, Reason: desc}
	}, approval.Deny)
	if !attached {
		return approval.Deny, nil
	}
	return d, err
}

// Resolve delivers a user decision back to the blocked Request call. Called by
// the Model when an ApprovalDecisionMsg arrives. Unknown ids are dropped.
func (a *Approver) Resolve(useID string, d ApprovalDecision) {
	a.resolve(useID, modalDecisionToApproval(d))
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
