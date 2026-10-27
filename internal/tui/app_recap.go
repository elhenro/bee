package tui

import (
	"context"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/learn"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

// recapTimeout caps the side-call so a stuck provider can't drag the turn
// past the loader fading out. Conservative — most recaps complete in a
// second or two against deepseek-v4-flash class models.
const recapTimeout = 20 * time.Second

// recapIdleDelay defers the side-call until the user has been idle for
// this long after a turn finishes. New submits bump m.recapGen and the
// scheduled tick drops itself when its captured gen no longer matches.
const recapIdleDelay = 3 * time.Second

// recapReadyMsg carries the synthesized one-line recap back into Update so
// it can be flushed into scrollback as a dim italic post-turn line. text
// non-empty = render recap; err non-empty = render error so the user sees why
// the side-call failed; both empty = the model declined, render nothing.
type recapReadyMsg struct {
	text string
	err  string
}

// recapIdleTickMsg fires recapIdleDelay after a turn finished. gen is the
// m.recapGen value captured when the tick was scheduled — a new submit
// since then bumps m.recapGen so the firing tick drops itself instead of
// kicking off the side-call. msgs is the snapshot to summarise.
type recapIdleTickMsg struct {
	gen  int
	msgs []types.Message
}

// recapIdleCmd schedules a recapIdleTickMsg after recapIdleDelay carrying
// the supplied gen and msgs.
func recapIdleCmd(gen int, msgs []types.Message) tea.Cmd {
	return tickAfter(recapIdleDelay, recapIdleTickMsg{gen: gen, msgs: msgs})
}

// recapMinDuration / recapMinTextLen gate the recap heuristic. Tuned so a
// quick greeting or one-liner Q&A skips the side call entirely; only turns
// that did real work (tool use, long reply, or wall-clock >= threshold)
// trigger a recap.
const (
	recapMinDuration = 15 * time.Second
	recapMinTextLen  = 600
)

// recapWorthwhile reports whether the just-finished turn merits a recap.
// True if any tool was used, the turn ran long, or the assistant produced
// a substantive reply. Scans the trailing assistant run only — stops at
// the previous user message, matching extractRecapInput's scope.
func recapWorthwhile(msgs []types.Message, dur time.Duration) bool {
	if dur >= recapMinDuration {
		return true
	}
	var textLen int
	for _, m := range slices.Backward(msgs) {

		if m.Role == types.RoleUser {
			break
		}
		if m.Role != types.RoleAssistant {
			continue
		}
		for _, c := range m.Content {
			switch c.Type {
			case types.BlockToolUse:
				return true
			case types.BlockText:
				textLen += len(c.Text)
			}
		}
	}
	return textLen >= recapMinTextLen
}

// recapCmd kicks off a side-LLM summarization of the just-finished turn and
// publishes recapReadyMsg with the result. Runs detached from the model's
// long-lived ctx so a cancellation of the next user input doesn't abort the
// recap mid-call; bounded by recapTimeout instead.
func (m Model) recapCmd(msgs []types.Message) tea.Cmd {
	if m.eng == nil || m.eng.Provider == nil {
		return nil
	}
	prov := m.eng.Provider
	model := m.eng.Cfg.DefaultModel
	copyMsgs := append([]types.Message(nil), msgs...)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), recapTimeout)
		defer cancel()
		r := loop.GenerateRecap(ctx, prov, model, copyMsgs)
		msg := recapReadyMsg{text: r.Text}
		if r.Err != nil {
			msg.err = r.Err.Error()
		}
		return msg
	}
}

// changeMapMsg carries the end-of-turn review card back into Update.
type changeMapMsg struct{ text string }

// changeMapCmd builds the review card for the turn's file mutations. It is
// local work (git + code index), so it needs no model call and shows as soon
// as the turn ends. A turn that mutated nothing costs no command at all.
func (m Model) changeMapCmd(msgs []types.Message) tea.Cmd {
	turn := slices.Clone(learn.LastTurn(msgs))
	if m.eng == nil || len(learn.Touched(turn, "")) == 0 {
		return nil
	}
	cwd := m.eng.Cwd
	return func() tea.Msg {
		return changeMapMsg{text: learn.ChangeMap(context.Background(), cwd, turn)}
	}
}
