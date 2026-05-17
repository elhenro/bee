package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// registerBackground adds background-bee commands: bg, agent, attach.
func registerBackground(r *Registry) {
	r.Register(Command{
		Name:        "bg",
		Description: "spawn a background bee: /bg <task>",
		Run: func(_ context.Context, args []string, _ Side) (string, error) {
			if len(args) == 0 {
				return "usage: /bg <task>", nil
			}
			self, err := os.Executable()
			if err != nil {
				return "", err
			}
			out, err := exec.Command(self, "bg", strings.Join(args, " ")).CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("/bg: %v: %s", err, out)
			}
			return strings.TrimSpace(string(out)), nil
		},
	})
	r.Register(Command{
		Name:           "agent",
		Description:    "open agent view (background bees)",
		AllowDuringRun: true,
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			return "", s.OpenAgentView()
		},
	})
	r.Register(Command{
		Name:           "attach",
		Description:    "attach to a background session: /attach <id>",
		AllowDuringRun: true,
		Run: func(_ context.Context, args []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			if len(args) == 0 {
				return "usage: /attach <session-id>", nil
			}
			return "", s.OpenSession(args[0])
		},
	})
	r.Register(Command{
		Name:           "agents",
		Description:    "parallel-agents overview — open in a new shell: `bee agents`",
		AllowDuringRun: true,
		Run: func(_ context.Context, _ []string, _ Side) (string, error) {
			return "Quit (ctrl+d ctrl+d) and run `bee agents` to open the parallel-agents overview. Each chat message there spawns a new agent in its own git worktree.", nil
		},
	})
}
