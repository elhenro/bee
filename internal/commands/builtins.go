package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RegisterBuiltins adds the default /compact /model /resume /new /copy /quit /help commands.
func RegisterBuiltins(r *Registry) {
	r.Register(Command{
		Name:        "compact",
		Description: "summarize old turns to free context",
		Run: func(ctx context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			return "", s.Compact(ctx)
		},
	})
	r.Register(Command{
		Name:           "model",
		Description:    "interactive model picker — /model or /model <id>",
		AllowDuringRun: true,
		Run: func(_ context.Context, args []string, s Side) (string, error) {
			if s == nil {
				return "usage: /model [id]", nil
			}
			if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
				if err := s.OpenPicker(); err == nil {
					return "", nil
				}
				return "usage: /model <id>   (or open the interactive picker)", nil
			}
			return "", s.SwitchModel(args[0])
		},
	})
	r.Register(Command{
		Name:        "resume",
		Description: "browse and resume a past session",
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			ids, err := s.ListSessions()
			if err != nil {
				return "", err
			}
			if len(ids) == 0 {
				return "no past sessions", nil
			}
			if err := s.OpenResume(); err == nil {
				return "", nil
			}
			return "sessions:\n" + strings.Join(ids, "\n"), nil
		},
	})
	r.Register(Command{
		Name:        "new",
		Description: "start a fresh session",
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			return "", s.NewSession()
		},
	})
	// alias for /new
	r.Register(Command{
		Name:        "clear",
		Description: "start a fresh session (alias of /new)",
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			return "", s.NewSession()
		},
	})
	r.Register(Command{
		Name:        "copy",
		Description: "copy last assistant message to clipboard",
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			return "", s.CopyLast()
		},
	})
	r.Register(Command{
		Name:        "quit",
		Description: "quit bee",
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			s.Quit()
			return "", nil
		},
	})
	// alias for /quit — familiar to users coming from shells
	r.Register(Command{
		Name:        "exit",
		Description: "quit bee (alias of /quit)",
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			s.Quit()
			return "", nil
		},
	})
	r.Register(Command{
		Name:           "tree",
		Description:    "open session tree view",
		AllowDuringRun: true,
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			return "", s.OpenTree()
		},
	})
	r.Register(Command{
		Name:           "cost",
		Description:    "open cost monitor",
		AllowDuringRun: true,
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			return "", s.OpenCost()
		},
	})
	r.Register(Command{
		Name:        "fork",
		Description: "fork a new session — /fork [msgID]",
		Run: func(_ context.Context, args []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			from := ""
			if len(args) > 0 {
				from = args[0]
			}
			return "", s.ForkSession(from)
		},
	})
	r.Register(Command{
		Name:        "clone",
		Description: "clone current session into a new one",
		Run: func(_ context.Context, _ []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			return "", s.CloneSession()
		},
	})
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
			return "", s.Login(ctx, name)
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
	r.Register(Command{
		Name:           "settings",
		Description:    "toggle verbosity + agent-thought visibility (persisted)",
		AllowDuringRun: true,
		Run: func(_ context.Context, args []string, s Side) (string, error) {
			if s == nil {
				return "", nil
			}
			if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
				if err := s.OpenSettings(); err == nil {
					return "", nil
				}
				return renderSettingsStatus(s), nil
			}
			return applySettingsArg(args, s)
		},
	})
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
		Name:           "help",
		Description:    "list slash commands",
		AllowDuringRun: true,
		Run: func(_ context.Context, _ []string, _ Side) (string, error) {
			return "/compact, /model, /effort, /settings, /resume, /new, /clear, /copy, /tree, /fork, /clone, /login, /logout, /bg, /agent, /attach, /quit, /exit, /help", nil
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

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func quote(s string) string { return "\"" + s + "\"" }

// renderSettingsStatus prints the current settings + headless usage hint.
func renderSettingsStatus(s Side) string {
	var b strings.Builder
	b.WriteString("settings:\n")
	b.WriteString("  verbose       ")
	b.WriteString(onOff(s.GetVerbose()))
	b.WriteString("  full tool output\n")
	b.WriteString("  show_thoughts ")
	b.WriteString(onOff(s.GetShowThoughts()))
	b.WriteString("  render agent reasoning blocks\n")
	b.WriteString("  show_nudges   ")
	b.WriteString(onOff(s.GetShowNudges()))
	b.WriteString("  show loop [nudge] recovery turns\n")
	b.WriteString("  compact       ")
	b.WriteString(onOff(s.GetCompact()))
	b.WriteString("  drop tui spacing (gutter, blank, tint, OSC 133)\n\n")
	b.WriteString("usage: /settings <key> <on|off>\n")
	b.WriteString("       /settings              (open pane)\n")
	return b.String()
}

// applySettingsArg handles `/settings <key> <on|off>` (and a few aliases).
// Single-arg toggle form (`/settings verbose`) flips the current value.
func applySettingsArg(args []string, s Side) (string, error) {
	key := strings.ToLower(args[0])
	switch key {
	case "verbose", "v", "verbosity":
		key = "verbose"
	case "thoughts", "thought", "think", "show_thoughts", "show-thoughts":
		key = "show_thoughts"
	case "nudge", "nudges", "show_nudges", "show-nudges":
		key = "show_nudges"
	case "compact", "dense", "tight":
		key = "compact"
	default:
		return "unknown setting " + quote(args[0]) + " (want: verbose | show_thoughts | show_nudges | compact)", nil
	}
	var newVal bool
	if len(args) >= 2 {
		v, ok := parseOnOff(args[1])
		if !ok {
			return "unknown value " + quote(args[1]) + " (want: on | off)", nil
		}
		newVal = v
	} else {
		// single-arg form: flip current value
		switch key {
		case "verbose":
			newVal = !s.GetVerbose()
		case "show_thoughts":
			newVal = !s.GetShowThoughts()
		case "show_nudges":
			newVal = !s.GetShowNudges()
		case "compact":
			newVal = !s.GetCompact()
		}
	}
	var err error
	switch key {
	case "verbose":
		err = s.SetVerbose(newVal)
	case "show_thoughts":
		err = s.SetShowThoughts(newVal)
	case "show_nudges":
		err = s.SetShowNudges(newVal)
	case "compact":
		err = s.SetCompact(newVal)
	}
	if err != nil {
		return "", err
	}
	return key + ": " + onOff(newVal), nil
}

func onOff(v bool) string {
	if v {
		return "on "
	}
	return "off"
}

func parseOnOff(s string) (bool, bool) {
	switch strings.ToLower(s) {
	case "on", "true", "1", "yes", "y":
		return true, true
	case "off", "false", "0", "no", "n":
		return false, true
	}
	return false, false
}
