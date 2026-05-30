package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap centralises every binding the TUI listens to. Other slices
// (workspace, hive, provider picker) just consume the same struct.
type KeyMap struct {
	Submit       key.Binding
	Quit         key.Binding
	Cancel       key.Binding
	ProviderPick key.Binding
	WorkspaceTog key.Binding
	HiveOpen     key.Binding
	SessionTree  key.Binding
	CostOpen     key.Binding
	SlashPalette  key.Binding
	HistorySearch key.Binding
	CavemanCycle  key.Binding
	ThinkingCycle key.Binding
	ModeCycle     key.Binding
	ApproveAllow   key.Binding
	ApproveSession key.Binding
	ApproveAlways  key.Binding
	ApproveDeny    key.Binding
	// SteerNow: enter while streaming — injects a mid-turn user steer.
	// shares the "enter" key with Submit; dispatch is state-dependent.
	SteerNow key.Binding
	// FollowUp: alt+enter queues a follow-up that fires after the current
	// turn finishes (works in idle or streaming).
	FollowUp key.Binding
	// ImagePaste: ctrl+i stages an image from the system clipboard for the
	// next submit. bubbletea's raw-paste support is unreliable in many
	// terminals, hence the explicit chord.
	ImagePaste key.Binding
}

// DefaultKeyMap returns the default keyboard chord set.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Submit: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "submit"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c", "ctrl+d"),
			key.WithHelp("ctrl+c/ctrl+d", "quit"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel turn"),
		),
		ProviderPick: key.NewBinding(
			key.WithKeys("ctrl+p"),
			key.WithHelp("ctrl+p", "model picker"),
		),
		WorkspaceTog: key.NewBinding(
			key.WithKeys("ctrl+w"),
			key.WithHelp("ctrl+w", "workspace pane"),
		),
		HiveOpen: key.NewBinding(
			key.WithKeys("ctrl+h"),
			key.WithHelp("ctrl+h", "hive view"),
		),
		SessionTree: key.NewBinding(
			key.WithKeys("ctrl+t"),
			key.WithHelp("ctrl+t", "session tree"),
		),
		CostOpen: key.NewBinding(
			key.WithKeys("ctrl+y"),
			key.WithHelp("ctrl+y", "cost monitor"),
		),
		SlashPalette: key.NewBinding(
			key.WithKeys("ctrl+k"),
			key.WithHelp("ctrl+k", "command palette"),
		),
		HistorySearch: key.NewBinding(
			key.WithKeys("ctrl+r"),
			key.WithHelp("ctrl+r", "search history (fzf)"),
		),
		CavemanCycle: key.NewBinding(
			// ctrl+/ comes through as ctrl+_ on many terminals
			key.WithKeys("ctrl+/", "ctrl+_"),
			key.WithHelp("ctrl+/", "cycle caveman"),
		),
		ThinkingCycle: key.NewBinding(
			key.WithKeys("alt+t"),
			key.WithHelp("alt+t", "cycle thinking"),
		),
		ModeCycle: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "cycle mode (plan/auto/edit/yolo)"),
		),
		ApproveAllow: key.NewBinding(
			key.WithKeys("a", "y", "enter"),
			key.WithHelp("a/y", "allow once"),
		),
		ApproveSession: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "allow this session"),
		),
		ApproveAlways: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "allow forever (saved)"),
		),
		ApproveDeny: key.NewBinding(
			key.WithKeys("d", "n", "esc"),
			key.WithHelp("d/n", "deny"),
		),
		SteerNow: key.NewBinding(
			key.WithKeys("enter"), // same as Submit; state decides which fires
			key.WithHelp("enter", "steer mid-turn (while streaming)"),
		),
		FollowUp: key.NewBinding(
			key.WithKeys("alt+enter"),
			key.WithHelp("alt+enter", "queue follow-up"),
		),
		ImagePaste: key.NewBinding(
			key.WithKeys("ctrl+i"),
			key.WithHelp("ctrl+i", "paste image from clipboard"),
		),
	}
}
