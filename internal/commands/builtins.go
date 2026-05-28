package commands

import (
	"context"
	"strings"
)

// RegisterBuiltins adds the default /compact /model /resume /new /copy /quit /help commands.
func RegisterBuiltins(r *Registry) {
	registerSession(r)
	registerModel(r)
	registerLogin(r)
	registerSettings(r)
	registerTools(r)
	registerBackground(r)
	registerGoal(r)
	registerRemoteControl(r)
	r.Register(Command{
		Name:           "help",
		Description:    "list slash commands",
		AllowDuringRun: true,
		Run: func(_ context.Context, _ []string, _ Side) (string, error) {
			return "/compact, /model, /effort, /settings, /tools, /resume, /new, /clear, /copy, /tree, /fork, /clone, /login, /logout, /bg, /agent, /attach, /goal, /remote-control, /quit, /exit, /help", nil
		},
	})
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func quote(s string) string { return "\"" + s + "\"" }
