// Package tui implements bee's interactive Bubbletea interface.
//
// Palette follows the charmbracelet/crush approach: a layered neutral scale
// (multiple foreground subtleties × multiple background layers) with a single
// brand accent. Bee swaps crush's purple for honey amber. Hex values are
// inlined so we don't take a charmtone dependency.
package tui

import "github.com/charmbracelet/lipgloss"

// Layered neutrals — borrowed from charmtone, mirrored for light mode so
// the same role (subtle/body/highlight) stays consistent across themes.
//
//	fg: Oyster (most subtle) → Squid → Smoke → Ash (base) → Butter (highlight)
//	bg: Pepper (base) → BBQ → Charcoal → Iron
var (
	fgOyster = lipgloss.AdaptiveColor{Light: "#7C7A86", Dark: "#605F6B"} // most subtle text
	fgSquid  = lipgloss.AdaptiveColor{Light: "#5D5B6A", Dark: "#858392"} // subtle text
	fgSmoke  = lipgloss.AdaptiveColor{Light: "#3F3E4A", Dark: "#BFBCC8"} // body-secondary
	fgAsh    = lipgloss.AdaptiveColor{Light: "#201F26", Dark: "#DFDBDD"} // body text
	fgButter = lipgloss.AdaptiveColor{Light: "#201F26", Dark: "#FFFAF1"} // on-accent highlights

	bgPepper   = lipgloss.AdaptiveColor{Light: "#F4F1EC", Dark: "#201F26"} // base
	bgBBQ      = lipgloss.AdaptiveColor{Light: "#E8E4DD", Dark: "#2D2C35"} // surface
	bgCharcoal = lipgloss.AdaptiveColor{Light: "#D2CEC6", Dark: "#3A3943"} // separator / soft border
	bgIron     = lipgloss.AdaptiveColor{Light: "#B8B4AC", Dark: "#4D4C57"} // emphasized surface

	// warm tint for user-prompt bubbles. Adaptive so it stays subtle in
	// both modes: dark amber on dark terminals, light honey-cream on light
	// terminals. Either way the eye locks onto past user turns when
	// scanning scrollback without the block looking like a brutal slab.
	bgUserHl = lipgloss.AdaptiveColor{Light: "#FFE9B8", Dark: "#3A2E14"}
)

// Brand + semantic accents. Light-mode pairs darken the hue so it carries
// over white-ish backgrounds without losing the role association.
var (
	accentHoney = lipgloss.AdaptiveColor{Light: "#A56F00", Dark: "#FFB000"} // primary brand — bee accent
	accentYou   = lipgloss.AdaptiveColor{Light: "#0064B5", Dark: "#00A4FF"} // Malibu — user role
	accentBee   = lipgloss.AdaptiveColor{Light: "#8A5A00", Dark: "#FFC857"} // soft honey — assistant role
	accentTool  = lipgloss.AdaptiveColor{Light: "#6B4A93", Dark: "#B084CC"} // lilac — tool calls/output, distinct from honey + blue
	accentBusy  = lipgloss.AdaptiveColor{Light: "#5E6E00", Dark: "#E8FF27"} // Citron — streaming pulse
	semSuccess  = lipgloss.AdaptiveColor{Light: "#007A52", Dark: "#00FFB2"} // Julep
	semWarning  = lipgloss.AdaptiveColor{Light: "#7C7300", Dark: "#F5EF34"} // Mustard
	semError    = lipgloss.AdaptiveColor{Light: "#C0203F", Dark: "#EB4268"} // Sriracha
)

// Aliases kept so existing callers (caveman cycle, etc.) compile without churn.
var (
	colorHoney  = accentHoney
	colorBee    = accentBee
	colorYou    = accentYou
	colorTool   = fgSquid
	colorDim    = fgOyster
	colorBorder = bgCharcoal
	colorErr    = semError
	colorBg     = bgPepper
)

// Styles bundles every reusable lipgloss style.
type Styles struct {
	TopBar     lipgloss.Style // dim chrome — bee glyph carries the accent
	BottomBar  lipgloss.Style
	Scope      lipgloss.Style
	RoleYou    lipgloss.Style
	RoleBee    lipgloss.Style
	RoleTool   lipgloss.Style
	ToolCard   lipgloss.Style
	ToolName   lipgloss.Style // lilac, bold — distinguishes from honey AI + blue user
	ToolArgs   lipgloss.Style
	ToolPrev   lipgloss.Style
	ToolRail   lipgloss.Style // lilac left rail beside tool-output lines
	DiffAdd    lipgloss.Style // green `+` line for inserted text in edit previews
	DiffDel    lipgloss.Style // red `-` line for removed text in edit previews
	DiffPath   lipgloss.Style // bold path header above diff body
	DiffMeta   lipgloss.Style // dimmed meta line (occurrence, anchors, hunk hdrs)
	Thought    lipgloss.Style // grayed italic — model's chain-of-thought
	Modal      lipgloss.Style
	ModalTitle lipgloss.Style
	Button     lipgloss.Style
	ButtonHot  lipgloss.Style
	Error      lipgloss.Style
	Dim        lipgloss.Style
	Body       lipgloss.Style // base prose
	UserBubble lipgloss.Style // (legacy) full-width warm tint — retained for callers
	UserRail   lipgloss.Style // blue left rail beside user-turn lines
}

// DefaultStyles returns the layered honey-on-charmtone palette.
func DefaultStyles() Styles {
	return Styles{
		// chrome stays quiet; accent only on the bee glyph
		TopBar:    lipgloss.NewStyle().Foreground(fgSmoke).Bold(true),
		BottomBar: lipgloss.NewStyle().Foreground(fgOyster),
		Scope:     lipgloss.NewStyle().Foreground(fgSquid).Italic(true),

		// role markers — single-glyph, single-color
		RoleYou:  lipgloss.NewStyle().Foreground(accentYou).Bold(true),
		RoleBee:  lipgloss.NewStyle().Foreground(accentBee).Bold(true),
		RoleTool: lipgloss.NewStyle().Foreground(accentTool),

		// tool cards — borderless. Lilac so eye instantly distinguishes
		// tool activity from honey-AI prose and blue-user input.
		ToolCard: lipgloss.NewStyle(),
		ToolName: lipgloss.NewStyle().Foreground(accentTool).Bold(true),
		ToolArgs: lipgloss.NewStyle().Foreground(fgSquid),
		ToolPrev: lipgloss.NewStyle().Foreground(fgSmoke),
		ToolRail: lipgloss.NewStyle().Foreground(accentTool),

		// diff lines for edit/write/apply_patch/hashline_edit previews — green
		// add, red del, accent path header, dimmed meta. Same palette as
		// `git diff --color` so users read it instantly.
		DiffAdd:  lipgloss.NewStyle().Foreground(semSuccess),
		DiffDel:  lipgloss.NewStyle().Foreground(semError),
		DiffPath: lipgloss.NewStyle().Foreground(accentTool).Bold(true),
		DiffMeta: lipgloss.NewStyle().Foreground(fgOyster).Italic(true),

		// chain-of-thought: most subtle fg + italic so it reads as
		// "background musing", visibly secondary to the answer.
		Thought: lipgloss.NewStyle().Foreground(fgOyster).Italic(true),

		// approval modal
		Modal:      lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(bgIron).Padding(1, 2),
		ModalTitle: lipgloss.NewStyle().Foreground(accentHoney).Bold(true),
		Button:     lipgloss.NewStyle().Foreground(fgSquid).Padding(0, 2),
		ButtonHot:  lipgloss.NewStyle().Foreground(fgButter).Background(accentHoney).Bold(true).Padding(0, 2),

		Error: lipgloss.NewStyle().Foreground(semError).Bold(true),
		Dim:   lipgloss.NewStyle().Foreground(fgOyster),
		Body:  lipgloss.NewStyle().Foreground(fgAsh),

		// legacy user-bubble — kept so callers that referenced it still compile.
		// renderer no longer applies it; user turns now use a left-rail layout.
		UserBubble: lipgloss.NewStyle().Background(bgUserHl),
		// blue rail beside every user-turn line. Mirrors ToolRail's role for
		// tool output — one column wide, role-colored, no background.
		UserRail: lipgloss.NewStyle().Foreground(accentYou),
	}
}
