package tui

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/elhenro/bee/internal/atomicfile"
	"github.com/elhenro/bee/internal/beehome"
)

// historyMax caps the on-disk history file. Sized for years of prompts so
// fish-style autosuggestions can match against the user's whole history.
const historyMax = 50000

// HistorySelectMsg pastes the chosen entry into the main input.
type HistorySelectMsg struct{ Text string }

// HistoryDismissedMsg signals the picker closed without a pick.
type HistoryDismissedMsg struct{}

// HistoryPickerModel is the fzf-style reverse history search picker, rendered
// inline above the input bar in the same dense palette style as /model.
type HistoryPickerModel struct {
	Active   bool
	input    textinput.Model
	entries  []string // newest first, deduped
	matches  []fuzzy.Match
	selected int
	width    int
}

// NewHistoryPicker returns an inactive picker. Entries load lazily on Show.
func NewHistoryPicker() HistoryPickerModel {
	ti := textinput.New()
	ti.Placeholder = "type to filter history…"
	ti.Prompt = " › "
	ti.CharLimit = 256
	ti.Focus()
	return HistoryPickerModel{input: ti}
}

// SetWidth records terminal width so rows truncate cleanly. Mirrors palette/picker.
func (p *HistoryPickerModel) SetWidth(w int) { p.width = w }

// Show activates the picker, reloads history from disk, and resets the cursor.
func (p *HistoryPickerModel) Show(initial string) {
	p.Active = true
	p.entries = LoadHistory()
	p.selected = 0
	p.input.SetValue(initial)
	p.input.CursorEnd()
	p.input.Focus()
	p.recompute()
}

func (p *HistoryPickerModel) recompute() {
	needle := strings.TrimSpace(p.input.Value())
	if needle == "" {
		p.matches = make([]fuzzy.Match, len(p.entries))
		for i, e := range p.entries {
			p.matches[i] = fuzzy.Match{Index: i, Str: e}
		}
	} else {
		p.matches = fuzzy.Find(needle, p.entries)
	}
	if p.selected >= len(p.matches) {
		p.selected = 0
	}
}

// Update handles picker key events.
func (p HistoryPickerModel) Update(msg tea.Msg) (HistoryPickerModel, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc", "ctrl+c":
			p.Active = false
			return p, func() tea.Msg { return HistoryDismissedMsg{} }
		case "enter":
			if len(p.matches) == 0 {
				p.Active = false
				return p, func() tea.Msg { return HistoryDismissedMsg{} }
			}
			idx := p.selected
			if idx < 0 || idx >= len(p.matches) {
				idx = 0
			}
			text := p.entries[p.matches[idx].Index]
			p.Active = false
			return p, func() tea.Msg { return HistorySelectMsg{Text: text} }
		case "ctrl+r", "down", "ctrl+n":
			// ctrl+r cycles forward through results, fzf-style.
			if p.selected+1 < len(p.matches) {
				p.selected++
			}
			return p, nil
		case "up", "ctrl+p":
			if p.selected > 0 {
				p.selected--
			}
			return p, nil
		}
	}
	prev := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != prev {
		p.selected = 0
		p.recompute()
	}
	return p, cmd
}

// View renders the picker as a borderless dense strip — same aesthetic as
// the slash palette + /model picker. Crumb, filter input, palette rows, hint.
func (p HistoryPickerModel) View() string {
	if !p.Active {
		return ""
	}
	w := p.width
	if w <= 0 {
		w = 80
	}
	dim := lipgloss.NewStyle().Foreground(fgOyster)

	crumb := " " + StyleActive.Render("history") + dim.Render("  ⌃r search past prompts")

	body := renderRows(p.matches, p.selected, p.input.Value(), glyphSep, w, func(idx int) (string, string) {
		entry := p.entries[idx]
		// keep first line only; mark continuation so user knows the prompt is multi-line.
		// fuzzy match indices beyond the cut are dropped by renderRows' nameLen guard.
		more := ""
		if nl := strings.IndexByte(entry, '\n'); nl >= 0 {
			entry = entry[:nl]
			more = "  ↵ more"
		}
		return entry, more
	})

	hint := "enter pick · esc cancel · ↑↓ nav · ⌃r next"
	return strings.Join([]string{crumb, p.input.View(), body, " " + dim.Render(hint)}, "\n")
}

// ---- on-disk history ----

var historyMu sync.Mutex

// historyPath returns ~/.bee/history (honors BEE_HOME), "" when unresolvable.
func historyPath() string {
	p, _ := beehome.Join("history")
	return p
}

// readHistoryLines returns every line of the history file, nil when unreadable.
func readHistoryLines(p string) []string {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

// LoadHistory reads ~/.bee/history newest-first, dedupes consecutive entries.
func LoadHistory() []string {
	p := historyPath()
	if p == "" {
		return nil
	}
	historyMu.Lock()
	defer historyMu.Unlock()
	lines := readHistoryLines(p)
	// reverse to newest-first and dedupe globally — most-recent occurrence
	// wins so autosuggestions surface the freshest match for any prefix.
	out := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i] == "" {
			continue
		}
		l := decodeHistory(lines[i])
		if _, ok := seen[l]; ok {
			continue
		}
		seen[l] = struct{}{}
		out = append(out, l)
	}
	return out
}

// multiMark prefixes a stored entry that held newlines. Plain single-line
// entries are stored untouched, so older history files load unchanged.
const multiMark = "\x1e"

var (
	historyEscape   = strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`)
	historyUnescape = strings.NewReplacer(`\\`, `\`, `\n`, "\n", `\r`, "\r")
)

// encodeHistory folds a multi-line prompt onto one file line.
func encodeHistory(t string) string {
	if !strings.ContainsAny(t, "\n\r") {
		return t
	}
	return multiMark + historyEscape.Replace(t)
}

func decodeHistory(l string) string {
	if rest, ok := strings.CutPrefix(l, multiMark); ok {
		return historyUnescape.Replace(rest)
	}
	return l
}

// AppendHistory atomically appends a single entry to ~/.bee/history. Skips
// empty / whitespace-only lines. Trims to historyMax if the file gets large.
func AppendHistory(text string) {
	t := strings.TrimSpace(text)
	if t == "" {
		return
	}
	p := historyPath()
	if p == "" {
		return
	}
	historyMu.Lock()
	defer historyMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(encodeHistory(t) + "\n")
	_ = f.Close()
	maybeTrim(p)
}

// maybeTrim shrinks the history file when it exceeds historyMax lines.
// Stat-gated: AppendHistory runs synchronously in the update loop, and a
// full line scan per submit causes visible input lag once the file is big.
// Below ~8 bytes/line the file can't meaningfully exceed historyMax, and
// trim is best-effort housekeeping anyway.
func maybeTrim(p string) {
	if fi, err := os.Stat(p); err != nil || fi.Size() < int64(historyMax)*8 {
		return
	}
	lines := readHistoryLines(p)
	if len(lines) <= historyMax {
		return
	}
	keep := strings.Join(lines[len(lines)-historyMax:], "\n") + "\n"
	_ = atomicfile.Write(p, []byte(keep), 0o644)
}
