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
	registerInit(r)
	registerTUIOnly(r)
	r.Register(Command{
		Name:           "help",
		Description:    "list slash commands",
		AllowDuringRun: true,
		Run: func(_ context.Context, _ []string, _ Side) (string, error) {
			return helpText(r), nil
		},
	})
}

// runFunc is the type of Command.Run.
type runFunc = func(ctx context.Context, args []string, s Side) (string, error)

// withSide makes a nil Side a no-op.
func withSide(f runFunc) runFunc {
	return func(ctx context.Context, args []string, s Side) (string, error) {
		if s == nil {
			return "", nil
		}
		return f(ctx, args, s)
	}
}

// sideAct builds a command that calls act on the Side and prints nothing.
func sideAct(name, desc string, duringRun bool, act func(Side) error) Command {
	return Command{Name: name, Description: desc, AllowDuringRun: duringRun,
		Run: withSide(func(_ context.Context, _ []string, s Side) (string, error) {
			return "", act(s)
		})}
}

// openPane builds a command that opens a pane; hint is the headless fallback.
func openPane(name, desc string, duringRun bool, open func(Side) error, hint string) Command {
	return Command{Name: name, Description: desc, AllowDuringRun: duringRun,
		Run: withSide(func(_ context.Context, _ []string, s Side) (string, error) {
			if open(s) != nil {
				return hint, nil
			}
			return "", nil
		})}
}

// textCmd builds a command that always prints out: the generic-dispatch
// fallback for commands the TUI handles itself.
func textCmd(name, desc string, duringRun bool, out string) Command {
	return Command{Name: name, Description: desc, AllowDuringRun: duringRun,
		Run: func(context.Context, []string, Side) (string, error) { return out, nil }}
}

// wantsHelp reports whether args ask for the usage text: none, or a help flag.
func wantsHelp(args []string) bool {
	return len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h"
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
