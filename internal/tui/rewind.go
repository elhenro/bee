package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/checkpoint"
	"github.com/elhenro/bee/internal/types"
)

// RewindMode selects what a rewind restores.
type RewindMode int

const (
	RewindBoth RewindMode = iota // code + conversation
	RewindConversation
	RewindCode
)

// rewindModes is the mode menu, in display order.
var rewindModes = []struct {
	mode  RewindMode
	label string
}{
	{RewindBoth, "code + conversation"},
	{RewindConversation, "conversation only"},
	{RewindCode, "code only"},
}

// RewindEntry is one turn in the rewind picker.
type RewindEntry struct {
	MsgID   string // turn's last message id (snapshot + fork key)
	Preview string // opening user prompt
	Stat    string // git --shortstat of the turn
	Age     string // relative age of the turn
	HasCode bool   // a code snapshot exists for this turn
}

// RewindPicker is a two-stage modal: pick a turn, then pick what to restore.
type RewindPicker struct {
	open     bool
	stage    int // 0 = pick turn, 1 = pick mode
	entries  []RewindEntry
	selected int
	modeSel  int
}

// openRewindMsg asks the host to open the rewind picker.
type openRewindMsg struct{}

// ToggleRewindPickerMsg flips visibility.
type ToggleRewindPickerMsg struct{}

// RewindSelectMsg carries the chosen turn + restore mode back to the host.
type RewindSelectMsg struct {
	MsgID string
	Mode  RewindMode
}

// RewindDismissedMsg fires when the user closes without selecting.
type RewindDismissedMsg struct{}

// NewRewindPicker returns an inactive picker.
func NewRewindPicker() *RewindPicker { return &RewindPicker{} }

// Open reports modal visibility.
func (p *RewindPicker) Open() bool { return p.open }

// Show opens the modal at the turn list.
func (p *RewindPicker) Show(entries []RewindEntry) {
	p.entries = entries
	p.open = true
	p.stage = 0
	p.selected = 0
	p.modeSel = 0
}

// Hide closes the modal.
func (p *RewindPicker) Hide() { p.open = false }

// Entries exposes the list (for tests).
func (p *RewindPicker) Entries() []RewindEntry { return p.entries }

// Update handles key events while open.
func (p *RewindPicker) Update(msg tea.Msg) (*RewindPicker, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok || !p.open {
		return p, nil
	}
	if p.stage == 1 {
		return p.updateMode(km)
	}
	return p.updateList(km)
}

func (p *RewindPicker) updateList(m tea.KeyMsg) (*RewindPicker, tea.Cmd) {
	switch m.String() {
	case "up", "k", "ctrl+p":
		if p.selected > 0 {
			p.selected--
		}
	case "down", "j", "ctrl+n":
		if p.selected+1 < len(p.entries) {
			p.selected++
		}
	case "home", "g":
		p.selected = 0
	case "end", "G":
		if n := len(p.entries); n > 0 {
			p.selected = n - 1
		}
	case "enter":
		if p.selected >= 0 && p.selected < len(p.entries) {
			p.stage = 1
			// default to conversation-only when no code snapshot exists.
			if !p.entries[p.selected].HasCode {
				p.modeSel = 1
			} else {
				p.modeSel = 0
			}
		}
	case "esc", "q":
		p.open = false
		return p, func() tea.Msg { return RewindDismissedMsg{} }
	}
	return p, nil
}

func (p *RewindPicker) updateMode(m tea.KeyMsg) (*RewindPicker, tea.Cmd) {
	switch m.String() {
	case "up", "k":
		if p.modeSel > 0 {
			p.modeSel--
		}
	case "down", "j":
		if p.modeSel+1 < len(rewindModes) {
			p.modeSel++
		}
	case "enter":
		e := p.entries[p.selected]
		mode := rewindModes[p.modeSel].mode
		p.open = false
		return p, func() tea.Msg { return RewindSelectMsg{MsgID: e.MsgID, Mode: mode} }
	case "esc", "q":
		p.stage = 0 // back to the list
	}
	return p, nil
}

// View renders the active stage.
func (p *RewindPicker) View(width, height int) string {
	if !p.open {
		return ""
	}
	if p.stage == 1 {
		return p.viewMode(width, height)
	}
	return p.viewList(width, height)
}

func (p *RewindPicker) viewList(width, height int) string {
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAccent)).
		Bold(true).
		Render("⬢ Rewind")
	if len(p.entries) == 0 {
		body := StyleLabel.Render("(no checkpoints yet)")
		return boxModal(title+"\n\n"+body+"\n\n"+StyleLabel.Render("esc close"), width, height)
	}
	inner := width - 4
	if inner < 30 {
		inner = 30
	}
	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")
	for i, e := range p.entries {
		b.WriteString(renderRewindRow(e, i == p.selected, inner))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(StyleLabel.Render("↑/↓ nav · enter pick · esc close"))
	return boxModal(b.String(), width, height)
}

func (p *RewindPicker) viewMode(width, height int) string {
	e := p.entries[p.selected]
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAccent)).
		Bold(true).
		Render("⬢ Rewind to: " + e.Preview)
	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")
	for i, opt := range rewindModes {
		cursor := "  "
		style := StyleLabel
		if i == p.modeSel {
			cursor = "▸ "
			style = StyleActive
		}
		label := opt.label
		if opt.mode != RewindConversation && !e.HasCode {
			label += " (no code snapshot)"
		}
		b.WriteString(cursor + style.Render(label) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(StyleLabel.Render("↑/↓ choose · enter restore · esc back"))
	return boxModal(b.String(), width, height)
}

func renderRewindRow(e RewindEntry, selected bool, width int) string {
	cursor := "  "
	style := StyleLabel
	if selected {
		cursor = "▸ "
		style = StyleActive
	}
	preview := strings.TrimSpace(strings.ReplaceAll(e.Preview, "\n", " "))
	if preview == "" {
		preview = "(empty)"
	}
	meta := e.Age
	if e.Stat != "" {
		meta += "  " + e.Stat
	}
	if !e.HasCode {
		meta += "  (no code)"
	}
	head := cursor + style.Render(padRightCells(meta, 24)) + "  "
	maxPrev := width - lipglossWidth(head)
	if maxPrev < 10 {
		maxPrev = 10
	}
	if lipglossWidth(preview) > maxPrev {
		preview = truncateVisible(preview, maxPrev-1) + "…"
	}
	return head + style.Render(preview)
}

// buildRewindEntries groups messages into turns (newest first) and joins each
// with its code snapshot stat. The snapshot key is the turn's last message id.
func buildRewindEntries(msgs []types.Message, store *checkpoint.Store) []RewindEntry {
	type turn struct {
		key, preview, age string
	}
	var turns []turn
	have := false
	for _, msg := range msgs {
		if msg.Ephemeral {
			continue
		}
		if msg.Role == types.RoleUser {
			turns = append(turns, turn{
				key:     msg.ID,
				preview: previewText(firstText(msg), 60),
				age:     humanAge(msg.Time),
			})
			have = true
		} else if have {
			turns[len(turns)-1].key = msg.ID // extend to the turn's last message
		}
	}
	shas := make([]string, len(turns))
	has := make([]bool, len(turns))
	if store != nil {
		for i, tn := range turns {
			if sha, ok := store.SnapshotFor(tn.key); ok {
				shas[i], has[i] = sha, true
			}
		}
	}
	out := make([]RewindEntry, 0, len(turns))
	for i := len(turns) - 1; i >= 0; i-- {
		e := RewindEntry{MsgID: turns[i].key, Preview: turns[i].preview, Age: turns[i].age, HasCode: has[i]}
		if has[i] && store != nil {
			from := ""
			for j := i - 1; j >= 0; j-- {
				if has[j] {
					from = shas[j]
					break
				}
			}
			if stat, err := store.DiffStat(from, shas[i]); err == nil {
				e.Stat = compactStat(stat)
			}
		}
		out = append(out, e)
	}
	return out
}

// compactStat shortens git's "N files changed, X insertions(+), Y deletions(-)".
func compactStat(s string) string {
	files, ins, del := 0, 0, 0
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		switch {
		case strings.Contains(part, "file"):
			files, _ = strconv.Atoi(strings.Fields(part)[0])
		case strings.Contains(part, "insertion"):
			ins, _ = strconv.Atoi(strings.Fields(part)[0])
		case strings.Contains(part, "deletion"):
			del, _ = strconv.Atoi(strings.Fields(part)[0])
		}
	}
	if files == 0 {
		return ""
	}
	return strconv.Itoa(files) + "f +" + strconv.Itoa(ins) + " -" + strconv.Itoa(del)
}
