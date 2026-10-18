package herdr

import (
	"context"
	"strings"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/ask"
)

const blockMsgMax = 80

type approver struct{ inner approval.Approver }

// WrapApprover marks the pane blocked while a person decides on a flagged
// command. Wrap the interactive approver itself, below any decision cache, so
// cached grants never flash a blocked badge. Returns a unchanged when
// reporting is off.
func WrapApprover(a approval.Approver) approval.Approver {
	if a == nil || Default() == nil {
		return a
	}
	return approver{inner: a}
}

func (a approver) Request(ctx context.Context, cmd, key, desc string) (approval.Decision, error) {
	defer Block("approve: " + oneLine(cmd))()
	return a.inner.Request(ctx, cmd, key, desc)
}

type asker struct{ inner ask.Asker }

// WrapAsker marks the pane blocked while ask_user waits for a pick. Returns a
// unchanged when reporting is off.
func WrapAsker(a ask.Asker) ask.Asker {
	if a == nil || Default() == nil {
		return a
	}
	return asker{inner: a}
}

func (a asker) Ask(ctx context.Context, q ask.Question) (ask.Answer, error) {
	text := q.Header
	if text == "" {
		text = q.Prompt
	}
	defer Block("question: " + oneLine(text))()
	return a.inner.Ask(ctx, q)
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > blockMsgMax {
		s = string(r[:blockMsgMax-1]) + "…"
	}
	return s
}
