package tui

import "github.com/charmbracelet/lipgloss"

// every symbol the ui is built from, in one place so a font that mangles
// one is a one-line swap
const (
	glyphUser   = "▎" // left bar of your turns
	glyphBee    = "⬢" // assistant marker + brand mark
	glyphTool   = "●" // tool-call bullet
	glyphResult = "⎿" // connector from a call to its result
	glyphThink  = "✻" // reasoning marker
	glyphFail   = "✗" // turn-level error marker
	glyphPrompt = "❯" // input prompt
	glyphShell  = "!" // input prompt in shell mode
	glyphRule   = "─" // hairline
	glyphGauge  = "━" // filled part of the context gauge
	glyphBranch = "⎇"
	glyphSel    = "❯" // selected picker row
	glyphSep    = "·" // inline separator in status rows and hints
	glyphMore   = "…"
	glyphGap    = "⋯" // unchanged lines skipped inside a diff
)

// verbs for built-in tools; unknown ids (user tools, skills) render as-is
var toolTitles = map[string]string{
	"read":          "Read",
	"write":         "Write",
	"edit":          "Update",
	"hashline_edit": "Update",
	"apply_patch":   "Patch",
	"bash":          "Bash",
	"search":        "Search",
	"glob":          "Glob",
	"ls":            "List",
	"godoc":         "Godoc",
	"codegraph":     "Codegraph",
	"web_search":    "Web search",
	"web_fetch":     "Fetch",
	"ask_user":      "Ask",
}

func toolTitle(name string) string {
	if t, ok := toolTitles[name]; ok {
		return t
	}
	return name
}

// selection marker for a pane row: honey chevron on the selected one, blank
// column of the same width otherwise
func selectMark(selected bool) string {
	if !selected {
		return "  "
	}
	return lipgloss.NewStyle().Foreground(accentHoney).Render(glyphSel) + " "
}
