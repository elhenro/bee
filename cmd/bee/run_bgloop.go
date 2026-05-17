// Background-loop runner for `bee run --bg-loop`. Drives turns across
// inbox polls, writes status sidecars, exits on ctx cancel.
package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/loop"
)

// runBgLoop persists the engine across turns: write status sidecar on every
// boundary, run a turn, write awaiting status with the assistant's final
// text, then poll the inbox for follow-up messages. Exits on ctx cancel.
func runBgLoop(ctx context.Context, eng *loop.Engine, sessID, firstMsg string) error {
	base := bgreg.Status{
		SessionID: sessID,
		PID:       os.Getpid(),
		Task:      firstMsg,
		Model:     eng.Cfg.DefaultModel,
		Cwd:       eng.Cwd,
		StartedAt: time.Now().UTC(),
	}

	msg := firstMsg
	var cursor int64
	for {
		s := base
		s.State = bgreg.StateActive
		s.UpdatedAt = time.Now().UTC()
		_ = bgreg.Write(s)

		res, err := eng.Run(ctx, msg)
		if err != nil {
			if ctx.Err() != nil {
				s.State = bgreg.StateDone
				s.UpdatedAt = time.Now().UTC()
				_ = bgreg.Write(s)
				return nil
			}
			s.State = bgreg.StateFailed
			s.LastResponse = err.Error()
			s.UpdatedAt = time.Now().UTC()
			_ = bgreg.Write(s)
			return err
		}

		s = base
		s.State = bgreg.StateAwaiting
		s.LastResponse = res.FinalText
		s.UpdatedAt = time.Now().UTC()
		_ = bgreg.Write(s)

		next, newCursor, err := waitForInbox(ctx, sessID, cursor)
		if err != nil {
			return err
		}
		cursor = newCursor
		if ctx.Err() != nil {
			s.State = bgreg.StateDone
			s.UpdatedAt = time.Now().UTC()
			_ = bgreg.Write(s)
			return nil
		}
		msg = next
	}
}

// waitForInbox polls the inbox until a message arrives or ctx is cancelled.
// Returns the concatenated text of all new messages and the advanced cursor.
func waitForInbox(ctx context.Context, sessID string, cursor int64) (string, int64, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", cursor, nil
		case <-ticker.C:
			msgs, nc, err := bgreg.InboxDrain(sessID, cursor)
			if err != nil {
				return "", cursor, err
			}
			if len(msgs) > 0 {
				var b strings.Builder
				for i, m := range msgs {
					if i > 0 {
						b.WriteString("\n\n")
					}
					b.WriteString(m.Text)
				}
				return b.String(), nc, nil
			}
		}
	}
}
