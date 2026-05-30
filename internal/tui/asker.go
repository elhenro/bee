package tui

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/ask"
)

// Asker implements ask.Asker by routing questions through the TUI picker.
//
// The ask_user tool runs on the engine goroutine: Ask sends an AskShowMsg to
// the bubbletea program, then blocks on a per-request reply channel. The Model
// shows the picker; once the user chooses, the AskAnswerMsg is fed back through
// Resolve which unblocks the caller.
//
// Construct with NewAsker; call SetProgram after tea.NewProgram returns. Until
// then every Ask auto-resolves via ask.Static so the loop never wedges.
type Asker struct {
	program *tea.Program

	mu      sync.Mutex
	nextID  atomic.Uint64
	pending map[string]chan ask.Answer
}

// NewAsker returns an unwired asker. Call SetProgram to attach the tea program.
func NewAsker() *Asker {
	return &Asker{pending: map[string]chan ask.Answer{}}
}

// SetProgram wires the tea.Program handle. Safe to call before or after the
// program is running.
func (a *Asker) SetProgram(p *tea.Program) {
	a.mu.Lock()
	a.program = p
	a.mu.Unlock()
}

// Ask blocks until the user answers in the TUI picker. Falls back to the
// recommended option (ask.Static) if the program is unset, and on context
// cancellation returns a dismissed answer.
func (a *Asker) Ask(ctx context.Context, q ask.Question) (ask.Answer, error) {
	a.mu.Lock()
	p := a.program
	a.mu.Unlock()
	if p == nil {
		return ask.Static{}.Ask(ctx, q)
	}

	id := fmt.Sprintf("ask-%d", a.nextID.Add(1))
	ch := make(chan ask.Answer, 1)
	a.mu.Lock()
	a.pending[id] = ch
	a.mu.Unlock()

	p.Send(AskShowMsg{UseID: id, Question: q})

	select {
	case ans := <-ch:
		return ans, nil
	case <-ctx.Done():
		a.mu.Lock()
		delete(a.pending, id)
		a.mu.Unlock()
		return ask.Answer{Index: -1, Dismissed: true}, ctx.Err()
	}
}

// Resolve delivers a user answer back to the blocked Ask call. Called by the
// Model when an AskAnswerMsg arrives. Unknown ids are dropped.
func (a *Asker) Resolve(useID string, ans ask.Answer) {
	a.mu.Lock()
	ch, ok := a.pending[useID]
	if ok {
		delete(a.pending, useID)
	}
	a.mu.Unlock()
	if ok {
		ch <- ans
	}
}

// AskShowMsg asks the TUI to surface the picker for a question.
type AskShowMsg struct {
	UseID    string
	Question ask.Question
}

// AskAnswerMsg is published once the user picks. The Model forwards it to
// Asker.Resolve to unblock the tool.
type AskAnswerMsg struct {
	UseID  string
	Answer ask.Answer
}
