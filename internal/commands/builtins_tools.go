package commands

import (
	"context"
	"fmt"
	"strings"
)

// registerTools adds the /tools command and its helpers.
func registerTools(r *Registry) {
	r.Register(Command{
		Name:           "tools",
		Description:    "toggle / add tools — /tools, /tools add NAME CMD..., /tools rm NAME",
		AllowDuringRun: true,
		Run: withSide(func(_ context.Context, args []string, s Side) (string, error) {
			if len(args) == 0 {
				if err := s.OpenToolsPane(); err == nil {
					return "", nil
				}
				return renderToolsStatus(s.ListTools()), nil
			}
			return applyToolsArg(args, s)
		}),
	})
}

// renderToolsStatus prints the headless tool listing — one line per tool,
// marked by enabled flag and source. Used when the TUI pane isn't reachable.
func renderToolsStatus(list []ToolInfo) string {
	if len(list) == 0 {
		return "no tools registered"
	}
	width := 0
	for _, t := range list {
		width = max(width, len(t.Name))
	}
	var b strings.Builder
	b.WriteString("tools:\n")
	for _, t := range list {
		marker := "[x]"
		if t.Disabled {
			marker = "[ ]"
		}
		src := "builtin"
		if t.UserDefined {
			src = "user   "
		}
		fmt.Fprintf(&b, "  %s  %s  %s", marker, padRight(t.Name, width), src)
		if t.Description != "" {
			b.WriteString("  " + t.Description)
		}
		b.WriteByte('\n')
	}
	b.WriteString("\nusage: /tools                         (open pane)\n")
	b.WriteString("       /tools enable|disable NAME     (toggle one)\n")
	b.WriteString("       /tools add NAME CMD...         (register shell alias)\n")
	b.WriteString("       /tools rm NAME                 (remove user tool)\n")
	return b.String()
}

// setToolDisabled flips one tool and reports its new state.
func setToolDisabled(s Side, name string, disabled bool) (string, error) {
	if err := s.SetToolDisabled(name, disabled); err != nil {
		return "", err
	}
	if disabled {
		return name + ": disabled", nil
	}
	return name + ": enabled", nil
}

// applyToolsArg dispatches the headless subcommands.
func applyToolsArg(args []string, s Side) (string, error) {
	sub := strings.ToLower(args[0])
	switch sub {
	case "list", "ls", "status":
		return renderToolsStatus(s.ListTools()), nil
	case "enable", "disable":
		if len(args) < 2 {
			return "usage: /tools " + sub + " NAME", nil
		}
		return setToolDisabled(s, args[1], sub == "disable")
	case "add":
		if len(args) < 3 {
			return "usage: /tools add NAME CMD [-- description]", nil
		}
		name := args[1]
		cmdStr, desc := splitToolDescription(strings.Join(args[2:], " "))
		if err := s.AddUserTool(name, cmdStr, desc); err != nil {
			return "", err
		}
		return name + ": added", nil
	case "rm", "remove", "delete":
		if len(args) < 2 {
			return "usage: /tools rm NAME", nil
		}
		if err := s.RemoveUserTool(args[1]); err != nil {
			return "", err
		}
		return args[1] + ": removed", nil
	}
	// bare `/tools NAME` toggles. Treat as flip — disabled→enabled and vice versa.
	for _, t := range s.ListTools() {
		if t.Name == args[0] {
			return setToolDisabled(s, args[0], !t.Disabled)
		}
	}
	return "unknown subcommand or tool " + quote(args[0]) + " (want: enable | disable | add | rm | <name>)", nil
}

// splitToolDescription accepts "cmd args -- description" and returns the
// command (left side) and description (right side). The separator is " -- "
// surrounded by spaces; if absent, the whole string is the command.
func splitToolDescription(s string) (string, string) {
	cmd, desc, _ := strings.Cut(s, " -- ")
	return strings.TrimSpace(cmd), strings.TrimSpace(desc)
}
