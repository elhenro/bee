package commands

import (
	"context"
	"strings"
)

// registerModel adds model + effort commands.
func registerModel(r *Registry) {
	r.Register(Command{
		Name:           "model",
		Description:    "interactive model picker — /model, /model <id>, or /model <provider>/<id>",
		AllowDuringRun: true,
		Run: func(_ context.Context, args []string, s Side) (string, error) {
			if s == nil {
				return "usage: /model [id]", nil
			}
			if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
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
		},
	})
	r.Register(Command{
		Name:           "effort",
		Description:    "set reasoning effort — /effort [off|low|medium|high]",
		AllowDuringRun: true,
		Run: func(_ context.Context, args []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
				if err := s.OpenEffortPicker(); err == nil {
					return "", nil
				}
				cur := s.GetThinking()
				if cur == "" {
					cur = "off"
				}
				return "effort: " + cur + " (usage: /effort <off|low|medium|high>)", nil
			}
			if err := s.SetThinking(args[0]); err != nil {
				return "", err
			}
			return "effort: " + s.GetThinking(), nil
		},
	})
}

// splitProviderModel parses "<provider>/<model>" when the prefix matches a
// configured provider. Returns (provider, model, true) on a hit. The Side
// gate is what keeps openrouter IDs (e.g. "anthropic/claude-3-5-sonnet")
// from being misread as provider switches.
func splitProviderModel(arg string, s Side) (string, string, bool) {
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
