package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap centralises every binding the TUI listens to. Other slices
// (workspace, hive, provider picker) just consume the same struct.
type KeyMap struct {
	Submit         key.Binding
	Cancel         key.Binding
	ProviderPick   key.Binding
	HiveOpen       key.Binding
	SessionTree    key.Binding
	CostOpen       key.Binding
	SlashPalette   key.Binding
	HistorySearch  key.Binding
	CavemanCycle   key.Binding
	RoleCycle      key.Binding
	YoloToggle     key.Binding
	ApproveAllow   key.Binding
	ApproveSession key.Binding
	ApproveAlways  key.Binding
	ApproveDeny    key.Binding
	// FollowUp: alt+enter queues a follow-up that fires after the current
	// turn finishes (works in idle or streaming).
	FollowUp key.Binding
	// ImagePaste: alt+v stages an image from the system clipboard for the
	// next submit. bubbletea's raw-paste support is unreliable in many
	// terminals, hence the explicit chord. ctrl+i is the tab byte and arrives
	// as "tab", so it can never match.
	ImagePaste key.Binding
	// ExpandOutput: ctrl+o re-prints the last tool result at the opposite
	// detail level (full ↔ collapsed preview). Doubles as resume for a
	// parked external job — resume wins when one is suspended.
	ExpandOutput key.Binding
}

// bind builds a binding from its help label, description and keys.
func bind(help, desc string, keys ...string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc))
}

// DefaultKeyMap returns the default keyboard chord set.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Submit:        bind("enter", "submit", "enter"),
		Cancel:        bind("esc", "cancel turn", "esc"),
		ProviderPick:  bind("ctrl+p", "model picker", "ctrl+p"),
		HiveOpen:      bind("ctrl+h", "hive view", "ctrl+h"),
		SessionTree:   bind("ctrl+t", "session tree", "ctrl+t"),
		CostOpen:      bind("ctrl+y", "cost monitor", "ctrl+y"),
		SlashPalette:  bind("ctrl+k", "command palette", "ctrl+k"),
		HistorySearch: bind("ctrl+r", "search history (fzf)", "ctrl+r"),
		// ctrl+/ comes through as ctrl+_ on many terminals
		CavemanCycle:   bind("ctrl+/", "cycle caveman", "ctrl+/", "ctrl+_"),
		RoleCycle:      bind("shift+tab", "cycle role (worker/scout/queen)", "shift+tab"),
		YoloToggle:     bind("alt+y", "toggle yolo (auto-approve)", "alt+y"),
		ApproveAllow:   bind("a/y", "allow once", "a", "y", "enter"),
		ApproveSession: bind("s", "allow this session", "s"),
		ApproveAlways:  bind("f", "allow forever (saved)", "f"),
		ApproveDeny:    bind("d/n", "deny", "d", "n", "esc"),
		FollowUp:       bind("alt+enter", "queue follow-up", "alt+enter"),
		ImagePaste:     bind("alt+v", "paste image from clipboard", "alt+v"),
		ExpandOutput:   bind("ctrl+o", "expand last tool output", "ctrl+o"),
	}
}
