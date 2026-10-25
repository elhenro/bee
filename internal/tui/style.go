// Package tui implements bee's interactive Bubbletea interface.
//
// The look is "honey minimal": a layered neutral scale (faint → subtle →
// body → bright foregrounds, plus a few quiet panel backgrounds) with one
// brand accent, honey amber, and a handful of semantic hues for diffs and
// status. Chrome stays dim; honey marks what is live or yours (prompt,
// your turns, the spinner, the selected row). Hex values are inlined so we
// don't take a palette dependency.
package tui

import (
	"fmt"
	"math"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Layered neutrals, mirrored for light mode so the same role (faint /
// subtle / body / bright) stays consistent across themes.
//
//	fg: Oyster (faint) → Squid (subtle) → Smoke (secondary) → Ash (body) → Butter (bright)
//	bg: Charcoal (separator) → Iron (emphasis)
var (
	fgOyster = lipgloss.AdaptiveColor{Light: "#8B8996", Dark: "#5F5E6A"} // faint: hints, rules
	fgSquid  = lipgloss.AdaptiveColor{Light: "#65636F", Dark: "#8B8997"} // subtle: args, results
	fgSmoke  = lipgloss.AdaptiveColor{Light: "#45434F", Dark: "#B9B6C2"} // secondary body
	fgAsh    = lipgloss.AdaptiveColor{Light: "#222127", Dark: "#E0DCDE"} // body text
	fgButter = lipgloss.AdaptiveColor{Light: "#17161B", Dark: "#FFFAF1"} // bright / on-accent

	bgCharcoal = lipgloss.AdaptiveColor{Light: "#D2CEC6", Dark: "#3A3943"} // separator / soft border
	bgIron     = lipgloss.AdaptiveColor{Light: "#B8B4AC", Dark: "#4D4C57"} // emphasized surface

	// warm panel behind your own turns: a whisper of amber so past prompts
	// pop out of scrollback without a heavy slab.
	bgUserHl = lipgloss.AdaptiveColor{Light: "#F6ECD0", Dark: "#2B261A"}

	// failed-command badge bg: pale rose over cream, deep wine over charcoal.
	bgErrorHl = lipgloss.AdaptiveColor{Light: "#FFD7DC", Dark: "#4A1F2A"}

	// refusal/denial badge bg: reads "blocked, not broken".
	bgWarnHl = lipgloss.AdaptiveColor{Light: "#FFEAB8", Dark: "#4A3A14"}

	// + / - rows in edit previews: low-saturation tints so a many-row hunk
	// reads as a quiet block, not a scream.
	bgDiffAddHl = lipgloss.AdaptiveColor{Light: "#DDF3DF", Dark: "#1B3324"}
	bgDiffDelHl = lipgloss.AdaptiveColor{Light: "#F6DFE0", Dark: "#3A1F26"}
)

// Brand + semantic accents. Light-mode pairs darken the hue so it carries
// over pale backgrounds without losing the role association.
var (
	accentHoney = lipgloss.AdaptiveColor{Light: "#A56F00", Dark: "#FFB000"} // brand: prompt, spinner, your turns
	accentBee   = lipgloss.AdaptiveColor{Light: "#8A5A00", Dark: "#FFC857"} // soft honey: assistant marker
	accentTool  = lipgloss.AdaptiveColor{Light: "#6B4A93", Dark: "#B79AD6"} // lilac: queen role, rare accents
	accentCode  = lipgloss.AdaptiveColor{Light: "#0E7A6C", Dark: "#7FD1C2"} // teal: inline code
	accentBusy  = lipgloss.AdaptiveColor{Light: "#7A6A00", Dark: "#F0D84A"} // context nearly full
	semSuccess  = lipgloss.AdaptiveColor{Light: "#0A7A4F", Dark: "#6FD6A0"}
	semWarning  = lipgloss.AdaptiveColor{Light: "#8A6A00", Dark: "#E6C35C"}
	semError    = lipgloss.AdaptiveColor{Light: "#C0203F", Dark: "#EE6A82"}
)

// a quiet background tint, truecolor only. the 256 color cube has no dark
// subtle green or red, so those terminals get the nearest slab (a saturated
// block behind every changed row): better to drop the wash and let the sign
// and number carry the change
func wash(c lipgloss.AdaptiveColor) lipgloss.Style {
	if lipgloss.ColorProfile() != termenv.TrueColor {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Background(c)
}

// the warm panel behind your turns. 256 color terminals get a neutral lift
// instead: the cube's nearest match to the truecolor tint sinks below the
// terminal background and reads as a black bar
func userPanel() lipgloss.AdaptiveColor {
	if lipgloss.ColorProfile() == termenv.TrueColor {
		return bgUserHl
	}
	return lipgloss.AdaptiveColor{Light: "230", Dark: "236"}
}

// alias for the prompt and picker spinner
var colorHoney = accentHoney

// Styles bundles every reusable lipgloss style.
type Styles struct {
	TopBar     lipgloss.Style // dim chrome: the bee glyph carries the accent
	BottomBar  lipgloss.Style
	Scope      lipgloss.Style
	RoleYou    lipgloss.Style
	RoleBee    lipgloss.Style
	RoleTool   lipgloss.Style
	ToolCard   lipgloss.Style
	ToolName   lipgloss.Style // bold body: the verb of a tool call
	ToolArgs   lipgloss.Style // subtle: what the call acts on
	ToolPrev   lipgloss.Style // subtle: result preview rows
	ToolRail   lipgloss.Style // faint connector beside result rows
	DiffAdd    lipgloss.Style // green `+` text in edit previews
	DiffDel    lipgloss.Style // red `-` text in edit previews
	DiffAddBg  lipgloss.Style // soft green wash behind a whole `+` row
	DiffDelBg  lipgloss.Style // soft red wash behind a whole `-` row
	DiffPath   lipgloss.Style // path header above a diff body
	DiffMeta   lipgloss.Style // dimmed meta line (anchors, gaps)
	Thought    lipgloss.Style // faint italic: the model's chain-of-thought
	Modal      lipgloss.Style
	ModalTitle lipgloss.Style
	Button     lipgloss.Style
	ButtonHot  lipgloss.Style
	Error      lipgloss.Style
	ErrorCmd   lipgloss.Style // failed-bash command on a red wash
	Warn       lipgloss.Style // refused/denied text (blocked, not broken)
	WarnCmd    lipgloss.Style // refused command on a yellow wash
	WarnBadge  lipgloss.Style // pill badge for the escalate card title
	WarnRail   lipgloss.Style // amber rail beside escalate body lines
	Dim        lipgloss.Style
	Body       lipgloss.Style // base prose
	UserBubble lipgloss.Style // warm panel behind your turns
	UserRail   lipgloss.Style // honey bar beside your turns
	UserBody   lipgloss.Style // your turn's text on the panel

	Rule     lipgloss.Style // hairline rules framing the prompt
	Key      lipgloss.Style // keycap in a hint (bright)
	Hint     lipgloss.Style // hint label (faint)
	Mark     lipgloss.Style // honey assistant marker
	ToolOK   lipgloss.Style // finished-call bullet
	ToolFail lipgloss.Style // failed-call bullet
	Select   lipgloss.Style // selected picker row
}

// DefaultStyles returns the honey-minimal palette.
func DefaultStyles() Styles {
	return Styles{
		// chrome stays quiet; accent only where something is live
		TopBar:    lipgloss.NewStyle().Foreground(fgSquid),
		BottomBar: lipgloss.NewStyle().Foreground(fgOyster),
		Scope:     lipgloss.NewStyle().Foreground(fgSquid).Italic(true),

		RoleYou:  lipgloss.NewStyle().Foreground(accentHoney).Bold(true),
		RoleBee:  lipgloss.NewStyle().Foreground(accentHoney).Bold(true),
		RoleTool: lipgloss.NewStyle().Foreground(fgSquid),

		// tool activity is neutral so it reads as a quiet lane next to the
		// answer: bold verb, subtle object, faint connector.
		ToolCard: lipgloss.NewStyle(),
		ToolName: lipgloss.NewStyle().Foreground(fgAsh).Bold(true),
		ToolArgs: lipgloss.NewStyle().Foreground(fgSquid),
		ToolPrev: lipgloss.NewStyle().Foreground(fgSquid),
		ToolRail: lipgloss.NewStyle().Foreground(fgOyster),

		DiffAdd:   lipgloss.NewStyle().Foreground(semSuccess),
		DiffDel:   lipgloss.NewStyle().Foreground(semError),
		DiffAddBg: wash(bgDiffAddHl),
		DiffDelBg: wash(bgDiffDelHl),
		DiffPath:  lipgloss.NewStyle().Foreground(fgSmoke),
		DiffMeta:  lipgloss.NewStyle().Foreground(fgOyster),

		// chain-of-thought: faint italic, visibly secondary to the answer.
		Thought: lipgloss.NewStyle().Foreground(fgOyster).Italic(true),

		Modal:      lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(bgIron).Padding(1, 2),
		ModalTitle: lipgloss.NewStyle().Foreground(accentHoney).Bold(true),
		Button:     lipgloss.NewStyle().Foreground(fgSquid).Padding(0, 2),
		ButtonHot:  lipgloss.NewStyle().Foreground(fgButter).Background(accentHoney).Bold(true).Padding(0, 2),

		Error:     lipgloss.NewStyle().Foreground(semError),
		ErrorCmd:  lipgloss.NewStyle().Foreground(fgAsh).Background(bgErrorHl),
		Warn:      lipgloss.NewStyle().Foreground(semWarning),
		WarnCmd:   lipgloss.NewStyle().Foreground(fgAsh).Background(bgWarnHl),
		WarnBadge: lipgloss.NewStyle().Foreground(fgButter).Background(accentHoney).Bold(true).Padding(0, 1),
		WarnRail:  lipgloss.NewStyle().Foreground(semWarning),
		Dim:       lipgloss.NewStyle().Foreground(fgOyster),
		Body:      lipgloss.NewStyle().Foreground(fgAsh),

		// your turns: honey bar on a warm panel, body-colored text.
		UserBubble: lipgloss.NewStyle().Background(userPanel()),
		UserRail:   lipgloss.NewStyle().Foreground(accentHoney).Background(userPanel()),
		UserBody:   lipgloss.NewStyle().Foreground(fgButter).Background(userPanel()),

		Rule:     lipgloss.NewStyle().Foreground(bgCharcoal),
		Key:      lipgloss.NewStyle().Foreground(fgSmoke),
		Hint:     lipgloss.NewStyle().Foreground(fgOyster),
		Mark:     lipgloss.NewStyle().Foreground(accentHoney).Bold(true),
		ToolOK:   lipgloss.NewStyle().Foreground(semSuccess),
		ToolFail: lipgloss.NewStyle().Foreground(semError),
		Select:   lipgloss.NewStyle().Foreground(accentHoney).Bold(true),
	}
}

// rainbowColorForFrame maps an animation frame to a fully-saturated rainbow
// color, sweeping the whole hue wheel about every 7s at the glow-tick cadence.
// Drives the mastermind input glow. Truecolor hex — lipgloss renders 24-bit
// where the terminal supports it and degrades gracefully where it doesn't.
func rainbowColorForFrame(frame int) lipgloss.Color {
	const steps = 60 // frames per full hue cycle (~7.2s at 120ms)
	idx := ((frame % steps) + steps) % steps
	hue := float64(idx) / float64(steps) * 360.0
	r, g, b := hsvToRGB(hue, 0.85, 1.0)
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", r, g, b))
}

// hsvToRGB converts h∈[0,360), s,v∈[0,1] to 8-bit RGB components.
func hsvToRGB(h, s, v float64) (uint8, uint8, uint8) {
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60.0, 2)-1))
	off := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return uint8((r + off) * 255), uint8((g + off) * 255), uint8((b + off) * 255)
}
