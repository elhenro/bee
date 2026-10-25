package tui

import (
	"errors"
	"strings"

	"github.com/elhenro/bee/internal/prompt"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/types"
	"github.com/google/uuid"
)

// ListSessions enumerates rollouts on disk. Newest first by Created time
// is good enough for the resume picker; bee/internal/session.List already
// returns sessions with timestamps.
func (s *tuiSide) ListSessions() ([]string, error) {
	sess, err := session.List()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(sess))
	for _, x := range sess {
		out = append(out, x.ID)
	}
	return out, nil
}

// OpenSession swaps the engine to a previously-recorded rollout and seeds
// scrollback with its messages. The old rollout is closed; the new one is
// reopened in append mode so subsequent turns continue the conversation.
func (s *tuiSide) OpenSession(id string) error {
	if s.m == nil || s.m.eng == nil {
		return errors.New("no engine")
	}
	if id == "" {
		return errors.New("open session: empty id")
	}
	if s.m.engineBusy() {
		return errEngineBusy
	}
	// ReadResume (not Read) collapses at the last checkpoint, so resuming a
	// compacted session doesn't re-seed the full raw history — that would ship
	// the whole pre-compaction log to the model on the first turn (a multi-minute
	// local prompt-processing hang + big token bill). Falls back to raw Read when
	// no checkpoint exists, so non-compacted sessions are unchanged.
	prior, err := session.ReadResume(id)
	if err != nil {
		return err
	}
	roll, err := session.Open(id)
	if err != nil {
		return err
	}
	if s.m.eng.Sessions != nil {
		_ = s.m.eng.Sessions.Close()
	}
	s.m.eng.Sessions = roll
	s.m.messages = prior
	// seed the cost tracker so the context-fill bar reflects the resumed
	// conversation immediately. without this, LastInput() carries the prior
	// session's value (or 0) until the next provider event lands — the bar
	// updates only after the first reply. mirrors what /compact does via
	// SetEstimatedInput. exact count overwrites this on the next Record.
	// reset first: it also clears the estimate set right after it
	if s.m.eng.Costs != nil {
		s.m.eng.Costs.Reset()
		s.m.eng.Costs.SetEstimatedInput(prompt.EstimateTokens(messagesText(prior)))
	}
	// resets printedCount so flush() emits the resumed transcript into terminal
	// scrollback. The slash-command caller (runSlash) calls flush after Run()
	// returns.
	s.m.resetView()
	return nil
}

// messagesText flattens a transcript into a single string for token
// estimation. Includes assistant/user text and tool-result bodies (the heavy
// part of a resumed context), skips ephemeral UI-only echoes.
func messagesText(msgs []types.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		if m.Ephemeral {
			continue
		}
		for _, b := range m.Content {
			if b.Text != "" {
				sb.WriteString(b.Text)
				sb.WriteByte('\n')
			}
			if b.Result != nil && b.Result.Content != "" {
				sb.WriteString(b.Result.Content)
				sb.WriteByte('\n')
			}
		}
	}
	return sb.String()
}

// OpenResume asks the TUI to display the interactive resume picker.
func (s *tuiSide) OpenResume() error {
	return s.arm(func(m *Model) { m.resumeRequested = true })
}

// OpenRewind asks the TUI to display the interactive rewind picker.
func (s *tuiSide) OpenRewind() error {
	return s.arm(func(m *Model) { m.rewindRequested = true })
}

// SaveCheckpoint snapshots the work-tree now, keyed to the latest message so it
// surfaces against the current turn in the rewind picker.
func (s *tuiSide) SaveCheckpoint(label string) (string, error) {
	if s.m == nil || s.m.checkpoints == nil {
		return "", errors.New("checkpoints disabled")
	}
	key := ""
	if n := len(s.m.messages); n > 0 {
		key = s.m.messages[n-1].ID
	}
	if key == "" {
		return "", errors.New("nothing to checkpoint yet")
	}
	return s.m.checkpoints.Snapshot(key, label)
}

// NewSession clears scrollback for a fresh conversation. Past content
// stays in the terminal scrollback (we can't unprint stdout); flush()
// resets its counter so future messages print correctly. Swaps the engine
// rollout to a fresh uuid and resets the cost tracker so the context-fill
// indicator drops back to 0% (without these, the next turn would re-send
// the prior conversation and LastInput would still reflect the old fill).
func (s *tuiSide) NewSession() error {
	if s.m == nil {
		return nil
	}
	if s.m.eng != nil && s.m.engineBusy() {
		return errEngineBusy
	}
	s.m.messages = nil
	s.m.resetView()
	if s.m.eng != nil {
		if s.m.eng.Sessions != nil {
			_ = s.m.eng.Sessions.Close()
		}
		roll, err := session.Open(uuid.NewString())
		if err != nil {
			return err
		}
		s.m.eng.Sessions = roll
		s.m.eng.InitialMessages = nil
		if s.m.eng.Costs != nil {
			s.m.eng.Costs.Reset()
		}
	}
	return nil
}

// ForkSession forks the active session at fromMsgID (or fully if empty) and
// swaps the engine's rollout to the new one. Existing scrollback is cleared.
func (s *tuiSide) ForkSession(fromMsgID string) error {
	if s.m == nil || s.m.eng == nil || s.m.eng.Sessions == nil {
		return errors.New("no active session")
	}
	if s.m.engineBusy() {
		return errEngineBusy
	}
	newR, err := session.Fork(s.m.eng.Sessions.ID(), fromMsgID)
	if err != nil {
		return err
	}
	_ = s.m.eng.Sessions.Close()
	s.m.eng.Sessions = newR
	s.m.messages = nil
	s.m.resetView()
	return nil
}

// CloneSession is Fork with no message id — copies the full session.
func (s *tuiSide) CloneSession() error {
	return s.ForkSession("")
}
