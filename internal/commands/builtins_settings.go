package commands

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// boolSetting is one on/off key of /settings. names lists the accepted
// spellings, the first being the canonical key; status is the one-line
// description in the headless listing (empty = not listed there).
type boolSetting struct {
	names  []string
	status string
	get    func(Side) bool
	set    func(Side, bool) error
}

// boolSettings is in listing order.
var boolSettings = []boolSetting{
	{[]string{"verbose", "v", "verbosity"}, "full tool output", Side.GetVerbose, Side.SetVerbose},
	{[]string{"show_thoughts", "thoughts", "thought", "think", "show-thoughts"}, "render agent reasoning blocks", Side.GetShowThoughts, Side.SetShowThoughts},
	{[]string{"show_nudges", "nudge", "nudges", "show-nudges"}, "show loop [nudge] recovery turns", Side.GetShowNudges, Side.SetShowNudges},
	{[]string{"show_recap", "recap", "recaps", "show-recap"}, "one-line side-LLM recap after each turn", Side.GetShowRecap, Side.SetShowRecap},
	{[]string{"compact", "dense", "tight"}, "drop tui spacing (gutter, blank, tint, OSC 133)", Side.GetCompact, Side.SetCompact},
	{[]string{"shell_bang_silent", "bang", "shell_bang", "bang_silent"}, "", Side.GetShellBangSilent, Side.SetShellBangSilent},
	{[]string{"show_banner", "banner", "intro"}, "", Side.GetShowBanner, Side.SetShowBanner},
	{[]string{"show_loader", "loader", "generating"}, "", Side.GetShowLoader, Side.SetShowLoader},
}

// outerPadNames are the accepted spellings of the one numeric setting.
var outerPadNames = []string{"outer_pad_lr", "padding", "outer_pad", "outer-pad"}

// registerSettings adds the /settings command and its helpers.
func registerSettings(r *Registry) {
	r.Register(Command{
		Name:           "settings",
		Description:    "toggle verbosity + agent-thought visibility (persisted)",
		AllowDuringRun: true,
		Run: withSide(func(_ context.Context, args []string, s Side) (string, error) {
			if wantsHelp(args) {
				if err := s.OpenSettings(); err == nil {
					return "", nil
				}
				return renderSettingsStatus(s), nil
			}
			return applySettingsArg(args, s)
		}),
	})
}

// renderSettingsStatus prints the current settings + headless usage hint.
func renderSettingsStatus(s Side) string {
	var b strings.Builder
	b.WriteString("settings:\n")
	for _, st := range boolSettings {
		if st.status != "" {
			fmt.Fprintf(&b, "  %-13s %s  %s\n", st.names[0], onOff(st.get(s)), st.status)
		}
	}
	fmt.Fprintf(&b, "  %-13s %d    blank cells left+right around the TUI (0-8, 0=off)\n\n", outerPadNames[0], s.GetOuterPadLR())
	b.WriteString("usage: /settings <key> <on|off>\n")
	b.WriteString("       /settings outer_pad_lr <0-8>\n")
	b.WriteString("       /settings              (open pane)\n")
	return b.String()
}

// applySettingsArg handles `/settings <key> <on|off>` (and a few aliases).
// Single-arg toggle form (`/settings verbose`) flips the current value.
func applySettingsArg(args []string, s Side) (string, error) {
	key := strings.ToLower(args[0])
	if slices.Contains(outerPadNames, key) {
		return applyOuterPad(args, s)
	}
	i := slices.IndexFunc(boolSettings, func(st boolSetting) bool { return slices.Contains(st.names, key) })
	if i < 0 {
		keys := make([]string, 0, len(boolSettings)+1)
		for _, st := range boolSettings {
			keys = append(keys, st.names[0])
		}
		keys = append(keys, outerPadNames[0])
		return "unknown setting " + quote(args[0]) + " (want: " + strings.Join(keys, " | ") + ")", nil
	}
	st := boolSettings[i]
	var newVal bool
	if len(args) >= 2 {
		v, ok := parseOnOff(args[1])
		if !ok {
			return "unknown value " + quote(args[1]) + " (want: on | off)", nil
		}
		newVal = v
	} else {
		newVal = !st.get(s) // single-arg form: flip current value
	}
	if err := st.set(s, newVal); err != nil {
		return "", err
	}
	return st.names[0] + ": " + onOff(newVal), nil
}

// applyOuterPad sets the numeric padding key; it has no flip semantics.
func applyOuterPad(args []string, s Side) (string, error) {
	if len(args) < 2 {
		return "outer_pad_lr needs a number 0-8 (e.g. /settings outer_pad_lr 2)", nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(args[1]))
	if err != nil {
		return "outer_pad_lr: want a number 0-8, got " + quote(args[1]), nil
	}
	n = min(max(n, 0), 8)
	if err := s.SetOuterPadLR(n); err != nil {
		return "", err
	}
	return "outer_pad_lr: " + strconv.Itoa(n), nil
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
