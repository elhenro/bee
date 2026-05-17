package commands

import (
	"context"
	"strings"
)

// registerSession adds session-lifecycle commands: compact, resume, new,
// clear, copy, quit, exit, tree, cost, fork, clone.
func registerSession(r *Registry) {
	r.Register(Command{
		Name:        "compact",
		Description: "summarize old turns to free context",
		Run: func(ctx context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			return "", s.Compact(ctx)
		},
	})
	r.Register(Command{
		Name:        "resume",
		Description: "browse and resume a past session",
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
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
		},
	})
	r.Register(Command{
		Name:        "new",
		Description: "start a fresh session",
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			return "", s.NewSession()
		},
	})
	// alias for /new
	r.Register(Command{
		Name:        "clear",
		Description: "start a fresh session (alias of /new)",
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			return "", s.NewSession()
		},
	})
	r.Register(Command{
		Name:        "copy",
		Description: "copy last assistant message to clipboard",
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			return "", s.CopyLast()
		},
	})
	r.Register(Command{
		Name:        "quit",
		Description: "quit bee",
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			s.Quit()
			return "", nil
		},
	})
	// alias for /quit — familiar to users coming from shells
	r.Register(Command{
		Name:        "exit",
		Description: "quit bee (alias of /quit)",
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			s.Quit()
			return "", nil
		},
	})
	r.Register(Command{
		Name:           "tree",
		Description:    "open session tree view",
		AllowDuringRun: true,
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			return "", s.OpenTree()
		},
	})
	r.Register(Command{
		Name:           "cost",
		Description:    "open cost monitor",
		AllowDuringRun: true,
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			return "", s.OpenCost()
		},
	})
	r.Register(Command{
		Name:        "fork",
		Description: "fork a new session — /fork [msgID]",
		Run: func(_ context.Context, args []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			from := ""
			if len(args) > 0 {
				from = args[0]
			}
			return "", s.ForkSession(from)
		},
	})
	r.Register(Command{
		Name:        "clone",
		Description: "clone current session into a new one",
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			return "", s.CloneSession()
		},
	})
}
