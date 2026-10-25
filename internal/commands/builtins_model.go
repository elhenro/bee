package commands

import (
	"context"
	"strconv"
	"strings"
)

// modelSide is the narrow surface used by /model. Kept separate from Side so
// other UI entrypoints can reuse the exact command behavior without stubbing
// every slash-command method.
type modelSide interface {
	SwitchModel(name string) error
	SwitchProviderModel(provider, model string) error
	OpenPicker() error
	LoginStatus() []ProviderAuth
}

// RunModelCommand implements /model. Used by the main TUI registry and any
// lighter UI that wants identical /model semantics.
func RunModelCommand(args []string, s modelSide) (string, error) {
	if s == nil {
		return "usage: /model [id]", nil
	}
	if wantsHelp(args) {
		if err := s.OpenPicker(); err == nil {
			return "", nil
		}
		return "usage: /model <id>   (or /model <provider>/<id>, or open the interactive picker)", nil
	}
	// <provider>/<id> form: swap provider and model in one shot, but
	// only when the prefix matches a configured provider. Avoids
	// hijacking openrouter IDs like "anthropic/claude-3-5-sonnet"
	// where the slash is part of the upstream model name.
	if p, m, ok := splitProviderModel(args[0], s); ok {
		return "", s.SwitchProviderModel(p, m)
	}
	return "", s.SwitchModel(args[0])
}

// registerModel adds model + role commands.
func registerModel(r *Registry) {
	r.Register(
		Command{
			Name:           "model",
			Description:    "interactive model picker — /model, /model <id>, or /model <provider>/<id>",
			AllowDuringRun: true,
			Run: func(_ context.Context, args []string, s Side) (string, error) {
				return RunModelCommand(args, s)
			},
		},
		Command{
			Name:           "role",
			Description:    "set agent role — /role [worker|scout|queen] (or open the picker)",
			AllowDuringRun: true,
			Run: withSide(func(_ context.Context, args []string, s Side) (string, error) {
				if wantsHelp(args) {
					if err := s.OpenRolePicker(); err == nil {
						return "", nil
					}
					return "role: " + s.GetRole() + " (usage: /role <worker|scout|queen>)", nil
				}
				if err := s.SetRole(args[0]); err != nil {
					return "", err
				}
				return "role: " + s.GetRole(), nil
			}),
		},
		Command{
			Name:           "effort",
			Description:    "pin reasoning budget — /effort <off|low|medium|high|max|auto>",
			AllowDuringRun: true,
			Run: withSide(func(_ context.Context, args []string, s Side) (string, error) {
				usage := func() string {
					return "effort: " + s.GetThinking() + " (usage: /effort <off|low|medium|high|max|auto>)"
				}
				if len(args) == 0 {
					if err := s.OpenEffortPicker(); err == nil {
						return "", nil
					}
					return usage(), nil
				}
				if wantsHelp(args) {
					return usage(), nil
				}
				if err := s.SetThinking(args[0]); err != nil {
					return "", err
				}
				return "effort: " + s.GetThinking(), nil
			}),
		},
		iterationsCmd("iterations", "set per-Run tool-use cap — /iterations <n>, /iterations 0 for unlimited"),
		iterationsCmd("iter", "alias for /iterations"),
	)
}

func iterationsCmd(name, desc string) Command {
	return Command{Name: name, Description: desc, AllowDuringRun: true, Run: withSide(runIterations)}
}

// runIterations backs /iterations and its /iter alias: prints the current cap
// on no args, otherwise sets it. 0 = unlimited; negatives clamp to 0.
func runIterations(_ context.Context, args []string, s Side) (string, error) {
	if wantsHelp(args) {
		return "iterations: " + fmtIterCap(s.GetMaxIterations()) +
			" (usage: /iterations <n>, /iterations 0 for unlimited)", nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil {
		return "iterations: want a number (got " + quote(args[0]) + "); /iterations 0 for unlimited", nil
	}
	if n < 0 {
		n = 0
	}
	if err := s.SetMaxIterations(n); err != nil {
		return "", err
	}
	return "iterations: " + fmtIterCap(n), nil
}

// fmtIterCap renders the iteration cap for display: 0 reads as "unlimited".
func fmtIterCap(n int) string {
	if n <= 0 {
		return "unlimited"
	}
	return strconv.Itoa(n)
}

// splitProviderModel parses "<provider>/<model>" when the prefix matches a
// configured provider. Returns (provider, model, true) on a hit. The Side
// gate is what keeps openrouter IDs (e.g. "anthropic/claude-3-5-sonnet")
// from being misread as provider switches.
func splitProviderModel(arg string, s modelSide) (string, string, bool) {
	i := strings.IndexByte(arg, '/')
	if i <= 0 || i == len(arg)-1 {
		return "", "", false
	}
	prefix := arg[:i]
	for _, p := range s.LoginStatus() {
		if p.Name == prefix {
			return prefix, arg[i+1:], true
		}
	}
	return "", "", false
}
