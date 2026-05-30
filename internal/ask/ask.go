// Package ask lets a tool pose a multiple-choice question to the user and
// block until they pick. Mirrors the approval gate: the TUI surfaces an
// interactive picker, headless runs auto-resolve so nothing ever hangs.
//
// Pattern: the ask_user tool builds a Question -> Asker.Ask surfaces it ->
// the user picks an option or types their own -> the chosen Answer flows back.
package ask

import "context"

// Option is one selectable choice. Recommended marks the model's suggested
// pick so the UI can highlight it and headless runs can auto-select it.
type Option struct {
	Label       string
	Description string
	Recommended bool
}

// Question is a single prompt with its choices. AllowCustom adds an escape
// hatch so the user can answer with free text instead of any listed option.
type Question struct {
	Header      string // short chip, e.g. "3D engine"
	Prompt      string // full question text
	Options     []Option
	AllowCustom bool
}

// Answer is the user's pick. Index points at the chosen Option, or -1 when the
// user typed custom text or dismissed. Text holds the chosen label or the
// custom string.
type Answer struct {
	Index     int
	Text      string
	Dismissed bool
}

// Asker surfaces a Question and blocks until the user answers. TUI shows a
// picker; headless returns the recommended option without prompting.
type Asker interface {
	Ask(ctx context.Context, q Question) (Answer, error)
}

// Static auto-resolves to the recommended option (or first) without
// prompting. Used for headless and autonomous runs where no human is present,
// so the tool never blocks the loop.
type Static struct{}

// Ask returns the recommended option, falling back to the first. Empty option
// lists resolve as dismissed.
func (Static) Ask(_ context.Context, q Question) (Answer, error) {
	if len(q.Options) == 0 {
		return Answer{Index: -1, Dismissed: true}, nil
	}
	idx := 0
	for i, o := range q.Options {
		if o.Recommended {
			idx = i
			break
		}
	}
	return Answer{Index: idx, Text: q.Options[idx].Label}, nil
}
