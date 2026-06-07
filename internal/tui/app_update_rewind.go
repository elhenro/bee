package tui

import (
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/prompt"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/types"
)

// onOpenRewind builds the turn list from the live conversation + checkpoints
// and shows the picker.
func (m Model) onOpenRewind(_ openRewindMsg) (tea.Model, tea.Cmd) {
	if m.rewind == nil {
		return m, nil
	}
	m.rewind.Show(buildRewindEntries(m.messages, m.checkpoints))
	return m, nil
}

// onRewindSelect restores code and/or conversation to the chosen turn.
func (m Model) onRewindSelect(msg RewindSelectMsg) (tea.Model, tea.Cmd) {
	var notes []string
	if msg.Mode == RewindCode || msg.Mode == RewindBoth {
		switch {
		case m.checkpoints == nil:
			notes = append(notes, "code disabled")
		default:
			if res, err := m.checkpoints.Restore(msg.MsgID); err != nil {
				notes = append(notes, "code failed")
			} else {
				note := "code→" + res.TargetSHA
				if s := compactStat(res.ShortStat); s != "" {
					note += " (" + s + ")"
				}
				notes = append(notes, note)
			}
		}
	}
	if msg.Mode == RewindConversation || msg.Mode == RewindBoth {
		nm, err := m.rewindConversation(msg.MsgID)
		if err != nil {
			notes = append(notes, "conversation failed")
		} else {
			m = nm
			notes = append(notes, "conversation")
		}
	}
	m.warning = "rewind: " + strings.Join(notes, " + ")
	m.warningExpires = time.Now().Add(warningTTL)
	return m, tea.Batch(m.flush(), warningFadeCmd())
}

// rewindConversation forks the session at toMsgID and reloads the trimmed
// history for display + context, mirroring OpenSession (the append-only log is
// never rewritten — Fork branches into a fresh session id).
func (m Model) rewindConversation(toMsgID string) (Model, error) {
	if m.eng == nil || m.eng.Sessions == nil {
		return m, errors.New("no active session")
	}
	newR, err := session.Fork(m.eng.Sessions.ID(), toMsgID)
	if err != nil {
		return m, err
	}
	prior := messagesUpTo(m.messages, toMsgID)
	_ = m.eng.Sessions.Close()
	m.eng.Sessions = newR
	m.messages = prior
	m.partial = ""
	m.streamFlushed = ""
	m.streamFenceOpen = false
	m.pendingFlushedPrefix = ""
	m.lastErr = ""
	m.state = StateIdle
	m.printedCount = 0
	if m.eng.Costs != nil {
		m.eng.Costs.SetEstimatedInput(prompt.EstimateTokens(messagesText(prior)))
	}
	return m, nil
}

// onCheckpointDone surfaces a per-turn snapshot failure; success is silent.
func (m Model) onCheckpointDone(msg checkpointDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.warning = "checkpoint failed: " + msg.err.Error()
		m.warningExpires = time.Now().Add(warningTTL)
		return m, warningFadeCmd()
	}
	return m, nil
}

// messagesUpTo returns a copy of msgs through the message with id (inclusive).
func messagesUpTo(msgs []types.Message, id string) []types.Message {
	for i, msg := range msgs {
		if msg.ID == id {
			return append([]types.Message(nil), msgs[:i+1]...)
		}
	}
	return msgs
}
