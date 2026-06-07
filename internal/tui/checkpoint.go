package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/types"
)

// maxCheckpoints bounds how many turn snapshots are retained per project.
const maxCheckpoints = 200

// checkpointDoneMsg reports the result of an async per-turn snapshot.
type checkpointDoneMsg struct {
	sha string
	err error
}

// snapshotAfterTurn captures the work-tree at the end of a turn, keyed by the
// turn's last message id so code and conversation rewind land at the same point.
// Best-effort and async; nil when checkpoints are disabled.
func (m Model) snapshotAfterTurn(msgs []types.Message) tea.Cmd {
	if m.checkpoints == nil || len(msgs) == 0 {
		return nil
	}
	// key on the last persisted (non-ephemeral) message so the picker's
	// per-turn key and session.Fork (which reads the rollout file) agree.
	key := lastPersistedID(msgs)
	if key == "" {
		return nil
	}
	label := turnLabel(msgs)
	store := m.checkpoints
	return func() tea.Msg {
		sha, err := store.Snapshot(key, label)
		if err == nil {
			_ = store.Prune(maxCheckpoints)
		}
		return checkpointDoneMsg{sha: sha, err: err}
	}
}

// lastPersistedID returns the id of the last non-ephemeral message (ephemeral
// UI echoes are never written to the rollout, so Fork can't anchor on them).
func lastPersistedID(msgs []types.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if !msgs[i].Ephemeral && msgs[i].ID != "" {
			return msgs[i].ID
		}
	}
	return ""
}

// turnLabel previews the user prompt that opened this turn, for the picker list.
func turnLabel(msgs []types.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == types.RoleUser {
			return previewText(firstText(msgs[i]), 60)
		}
	}
	return "snapshot"
}

// previewText collapses whitespace and truncates to n runes.
func previewText(s string, n int) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
