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
	registerBrowser(r)
	registerVision(r)
	registerBackground(r)
	registerGoal(r)
	registerInit(r)
	registerRemoteControl(r)
	registerStop(r)
	registerWatchdog(r)
	registerExternal(r)
	registerTutorial(r)
	registerHandoff(r)
	r.Register(Command{
		Name:           "help",
		Description:    "list slash commands",
		AllowDuringRun: true,
		Run: func(_ context.Context, _ []string, _ Side) (string, error) {
			return "/init, /compact, /model, /effort, /settings, /tools, /browser, /vision, /resume, /new, /clear, /copy, /tree, /cost, /usage, /fork, /clone, /login, /logout, /bg, /agent, /attach, /goal, /tutorial, /edit, /vim, /term, /remote-control, /stop, /watchdog, /handoff, /quit, /exit, /help", nil
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
