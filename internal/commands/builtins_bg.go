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
	r.Register(
		Command{
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
		},
		sideAct("agent", "open agent view (background bees)", true, Side.OpenAgentView),
		Command{
			Name:           "attach",
			Description:    "attach to a background session: /attach <id>",
			AllowDuringRun: true,
			Run: withSide(func(_ context.Context, args []string, s Side) (string, error) {
				if len(args) == 0 {
					return "usage: /attach <session-id>", nil
				}
				return "", s.OpenSession(args[0])
			}),
		},
	)
}
