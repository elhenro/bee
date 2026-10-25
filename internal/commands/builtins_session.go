package commands

import (
	"context"
	"strings"
)

const rewindHint = "rewind needs the interactive TUI"

// registerSession adds session-lifecycle commands: compact, resume, new,
// clear, copy, quit, exit, tree, cost, fork, clone.
func registerSession(r *Registry) {
	quit := func(s Side) error { s.Quit(); return nil }
	r.Register(
		Command{
			Name:        "compact",
			Description: "summarize old turns to free context",
			Run: withSide(func(ctx context.Context, _ []string, s Side) (string, error) {
				return "", s.Compact(ctx)
			}),
		},
		Command{
			Name:        "resume",
			Description: "browse and resume a past session",
			Run: withSide(func(_ context.Context, _ []string, s Side) (string, error) {
				ids, err := s.ListSessions()
				if err != nil {
					return "", err
				}
				if len(ids) == 0 {
					return "no past sessions", nil
				}
				if err := s.OpenResume(); err == nil {
					return "", nil
				}
				return "sessions:\n" + strings.Join(ids, "\n"), nil
			}),
		},
		openPane("rewind", "jump back to a previous code/conversation checkpoint", false, Side.OpenRewind, rewindHint),
		Command{
			Name:        "checkpoint",
			Description: "save a checkpoint now (/checkpoint save [label]) or browse them",
			Run: withSide(func(_ context.Context, args []string, s Side) (string, error) {
				if len(args) > 0 && args[0] == "save" {
					label := strings.TrimSpace(strings.Join(args[1:], " "))
					sha, err := s.SaveCheckpoint(label)
					if err != nil {
						return "", err
					}
					return "checkpoint saved: " + sha, nil
				}
				if err := s.OpenRewind(); err != nil {
					return rewindHint, nil
				}
				return "", nil
			}),
		},
		sideAct("new", "start a fresh session", false, Side.NewSession),
		// alias for /new
		sideAct("clear", "start a fresh session (alias of /new)", false, Side.NewSession),
		sideAct("copy", "copy last assistant message to clipboard", false, Side.CopyLast),
		sideAct("quit", "quit bee", false, quit),
		// alias for /quit — familiar to users coming from shells
		sideAct("exit", "quit bee (alias of /quit)", false, quit),
		sideAct("tree", "open session tree view", true, Side.OpenTree),
		sideAct("cost", "open cost monitor", true, Side.OpenCost),
		Command{
			Name:           "usage",
			Description:    "usage overview — day/week/month/all by provider & model",
			AllowDuringRun: true,
			Run: withSide(func(_ context.Context, _ []string, s Side) (string, error) {
				if err := s.OpenUsage(); err == nil {
					return "", nil
				}
				return s.UsageText(), nil
			}),
		},
		Command{
			Name:        "fork",
			Description: "fork a new session — /fork [msgID]",
			Run: withSide(func(_ context.Context, args []string, s Side) (string, error) {
				from := ""
				if len(args) > 0 {
					from = args[0]
				}
				return "", s.ForkSession(from)
			}),
		},
		sideAct("clone", "clone current session into a new one", false, Side.CloneSession),
	)
}
