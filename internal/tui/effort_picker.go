package tui

import tea "github.com/charmbracelet/bubbletea"

var effortLevels = []choiceLevel{
	{value: "auto", label: "auto", desc: "medium for reasoning models, off otherwise — the default"},
	{value: "off", label: "off", desc: "no thinking — fastest responses"},
	{value: "low", label: "low", desc: "minimal reasoning; disables thinking on local models"},
	{value: "medium", label: "medium", desc: "moderate reasoning"},
	{value: "high", label: "high", desc: "deep reasoning — best quality, slower"},
	{value: "max", label: "max", desc: "maximum budget — slowest, top quality"},
}

// EffortPane is a modal picker for the reasoning budget. Arrow keys pick, enter
// sets, esc closes without change. The pinned value overrides the role default.
type EffortPane = choicePane

// effortPickedMsg is published when the user commits an effort level.
type effortPickedMsg string

// NewEffortPane returns a closed effort picker.
func NewEffortPane() *EffortPane {
	return &choicePane{
		title:   "⬢ Reasoning Effort",
		levels:  effortLevels,
		picked:  func(v string) tea.Msg { return effortPickedMsg(v) },
		current: "auto",
	}
}
