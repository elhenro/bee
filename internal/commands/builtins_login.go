package commands

import (
	"context"
	"strings"
)

// registerLogin adds login + logout commands.
func registerLogin(r *Registry) {
	r.Register(Command{
		Name:        "login",
		Description: "interactive auth picker — /login or /login <provider>",
		Run: func(ctx context.Context, args []string, s Side) (string, error) {
			if s == nil {
				return "usage: /login <provider>", nil
			}
			if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
				if err := s.OpenLogin(); err == nil {
					return "", nil
				}
				return renderLoginStatus(s.LoginStatus()), nil
			}
			name := args[0]
			status := s.LoginStatus()
			match := findProvider(status, name)
			if match == nil {
				return "unknown provider " + quote(name) + "\n" + renderLoginStatus(status), nil
			}
			if !match.HasOAuth {
				// non-oauth providers: prefer the TUI pane (handles inline
				// api-key entry). Fall back to text hint when headless.
				if err := s.OpenLogin(); err == nil {
					return "", nil
				}
				return loginNoOAuthHint(*match), nil
			}
			if err := s.Login(ctx, name); err != nil {
				return "", err
			}
			return "logged in to " + name, nil
		},
	})
	r.Register(Command{
		Name:        "logout",
		Description: "remove stored OAuth token — /logout <provider>",
		Run: func(_ context.Context, args []string, s Side) (string, error) {
			if len(args) == 0 {
				return "usage: /logout <provider>", nil
			}
			if s == nil {
				return "", nil
			}
			return "", s.Logout(args[0])
		},
	})
}

// findProvider returns the entry matching name, or nil.
func findProvider(list []ProviderAuth, name string) *ProviderAuth {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

// renderLoginStatus produces a terse status table + usage hint.
// One line per provider: marker + name + auth method + state.
func renderLoginStatus(list []ProviderAuth) string {
	if len(list) == 0 {
		return "no providers configured. see ~/.bee/config.toml\nusage: /login <provider>"
	}
	width := 0
	for _, p := range list {
		if len(p.Name) > width {
			width = len(p.Name)
		}
	}
	var b strings.Builder
	b.WriteString("providers:\n")
	for _, p := range list {
		marker := "  "
		if p.IsDefault {
			marker = "* "
		}
		b.WriteString(marker)
		b.WriteString(padRight(p.Name, width))
		b.WriteString("  ")
		b.WriteString(authSummary(p))
		b.WriteByte('\n')
	}
	b.WriteString("\nusage: /login <provider>   (oauth flow if configured)\n")
	b.WriteString("       /logout <provider>  (remove saved token)\n")
	b.WriteString("legend: * default · ✓ ready · ○ needs setup\n")
	return b.String()
}

// authSummary describes how a provider authenticates and its current state.
func authSummary(p ProviderAuth) string {
	var parts []string
	if p.HasOAuth {
		if p.TokenSaved {
			parts = append(parts, "✓ oauth (token saved)")
		} else {
			parts = append(parts, "○ oauth (run /login "+p.Name+")")
		}
	}
	switch {
	case p.EnvKey == "":
		if !p.HasOAuth {
			parts = append(parts, "no auth (local)")
		}
	case p.EnvSet:
		parts = append(parts, "✓ env "+p.EnvKey)
	case p.KeySaved:
		parts = append(parts, "✓ key saved")
	case p.KeyOptional:
		parts = append(parts, "○ no key (optional)")
	default:
		parts = append(parts, "○ key (run /login "+p.Name+")")
	}
	return strings.Join(parts, " · ")
}

// loginNoOAuthHint explains the api-key path when oauth isn't configured.
// In TUI mode the login pane handles api-key entry inline; this text is the
// headless fallback for `/login <name>` when no pane is available.
func loginNoOAuthHint(p ProviderAuth) string {
	var b strings.Builder
	b.WriteString(p.Name)
	b.WriteString(": api-key auth (no oauth flow).\n")
	switch {
	case p.EnvKey == "":
		b.WriteString("provider has no env_key. add [providers." + p.Name + ".oauth] or env_key in ~/.bee/config.toml\n")
		return b.String()
	case p.EnvSet:
		b.WriteString("ready via env " + p.EnvKey + " — no /login needed.\n")
	case p.KeySaved:
		b.WriteString("key saved in ~/.bee/auth/" + p.Name + ".key — overwrite from the TUI /login pane or delete with /logout " + p.Name + ".\n")
	case p.KeyOptional:
		b.WriteString("key optional (server runs without auth by default). open the TUI and run /login " + p.Name + " to enroll one anyway, or `export " + p.EnvKey + "=...`.\n")
	default:
		b.WriteString("open the TUI and run /login " + p.Name + " to enter a key interactively, or `export " + p.EnvKey + "=...`.\n")
	}
	return b.String()
}
