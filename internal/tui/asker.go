package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/ask"
)

// Asker implements ask.Asker by routing questions through the TUI picker.
//
// The ask_user tool runs on the engine goroutine: Ask sends an AskShowMsg to
// the bubbletea program, then blocks until the Model shows the picker and the
// user chooses; the AskAnswerMsg is fed back through Resolve which unblocks the
// caller.
//
// Construct with NewAsker; call SetProgram after tea.NewProgram returns. Until
// then every Ask auto-resolves via ask.Static so the loop never wedges.
type Asker struct {
	*broker[ask.Answer]
}

// NewAsker returns an unwired asker. Call SetProgram to attach the tea program.
func NewAsker() *Asker {
	return &Asker{newBroker[ask.Answer]("ask")}
}

// Ask blocks until the user answers in the TUI picker. Falls back to the
// recommended option (ask.Static) if the program is unset, and on context
// cancellation returns a dismissed answer.
func (a *Asker) Ask(ctx context.Context, q ask.Question) (ask.Answer, error) {
	ans, attached, err := a.await(ctx, func(id string) tea.Msg {
		return AskShowMsg{UseID: id, Question: q}
	}, ask.Answer{Index: -1, Dismissed: true})
	if !attached {
		return ask.Static{}.Ask(ctx, q)
	}
	return ans, err
}

// Resolve delivers a user answer back to the blocked Ask call. Called by the
// Model when an AskAnswerMsg arrives. Unknown ids are dropped.
func (a *Asker) Resolve(useID string, ans ask.Answer) { a.resolve(useID, ans) }

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
