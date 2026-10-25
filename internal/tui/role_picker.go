package tui

import tea "github.com/charmbracelet/bubbletea"

var roleLevels = []choiceLevel{
	{value: "worker", label: "worker", desc: "full tool surface; per-turn read|act classifier — the everyday default"},
	{value: "scout", label: "scout", desc: "read-only research + web; proposes a plan, never mutates"},
	{value: "queen", label: "queen", desc: "spawn a hive — decompose, sub-agents, verify, synthesize · best quality, slowest, most tokens"},
}

// RolePane is a modal picker for the agent role. Arrow keys pick, enter sets,
// esc closes without change.
type RolePane = choicePane

// rolePickedMsg is published when the user commits a role.
type rolePickedMsg string

// NewRolePane returns a closed role picker.
func NewRolePane() *RolePane {
	return &choicePane{
		title:   "⬢ Agent Role",
		levels:  roleLevels,
		picked:  func(v string) tea.Msg { return rolePickedMsg(v) },
		current: "worker",
	}
}
