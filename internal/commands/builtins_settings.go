package commands

import (
	"context"
	"strings"
)

// registerSettings adds the /settings command and its helpers.
func registerSettings(r *Registry) {
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
}

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
	case "bang", "shell_bang", "shell_bang_silent", "bang_silent":
		key = "shell_bang_silent"
	case "banner", "show_banner", "intro":
		key = "show_banner"
	case "loader", "show_loader", "generating":
		key = "show_loader"
	default:
		return "unknown setting " + quote(args[0]) + " (want: verbose | show_thoughts | show_nudges | compact | shell_bang_silent | show_banner | show_loader)", nil
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
		case "shell_bang_silent":
			newVal = !s.GetShellBangSilent()
		case "show_banner":
			newVal = !s.GetShowBanner()
		case "show_loader":
			newVal = !s.GetShowLoader()
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
	case "shell_bang_silent":
		err = s.SetShellBangSilent(newVal)
	case "show_banner":
		err = s.SetShowBanner(newVal)
	case "show_loader":
		err = s.SetShowLoader(newVal)
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
