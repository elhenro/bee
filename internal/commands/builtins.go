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
			return helpText(r), nil
		},
	})
}

// one `/name  description` row per registered command, so the list can't
// drift from what the registry actually holds
func helpText(r *Registry) string {
	cmds := r.List()
	w := 0
	for _, c := range cmds {
		w = max(w, len(c.Name))
	}
	rows := make([]string, len(cmds))
	for i, c := range cmds {
		rows[i] = strings.TrimRight("/"+padRight(c.Name, w)+"  "+c.Description, " ")
	}
	return strings.Join(rows, "\n")
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func quote(s string) string { return "\"" + s + "\"" }
