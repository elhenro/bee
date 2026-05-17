package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
)

// PickedMsg is published when the user selects a provider+model combo.
type PickedMsg struct{ Provider, Model string }

// isAuthErr returns true when the fetched-models error message looks like
// an authentication failure. Conservative substring match — false negatives
// only hide the inline /login hint, never break the picker.
func isAuthErr(msg string) bool {
	low := strings.ToLower(msg)
	for _, needle := range []string{
		"status 401", "status 403",
		"authentication_error", "unauthorized", "forbidden",
		"api key required", "invalid api key", "missing api key",
	} {
		if strings.Contains(low, needle) {
			return true
		}
	}
	return false
}

// PickerDismissedMsg is published when the user hits Esc without picking.
type PickerDismissedMsg struct{}

// PickerLoginRequestedMsg is published when the user hits `l` from the
// picker's auth-error stage, asking the app to open the LoginPane on the
// failing provider.
type PickerLoginRequestedMsg struct{ Provider string }

// modelsLoadedMsg is internal: a /models fetch finished.
type modelsLoadedMsg struct {
	provider string
	models   []llm.Model
	err      error
}

// ModelLister is the seam the picker uses to fetch a provider's catalogue.
type ModelLister func(ctx context.Context, name string, cfg config.ProviderConfig) ([]llm.Model, error)

// pickerColumn enumerates which stage of the two-stage picker is active.
type pickerColumn int

const (
	colProviders pickerColumn = iota
	colModels
)

// maxPickerRows caps visible rows so the picker stays as compact as the
// slash palette — no rounded box, no empty padding.
const maxPickerRows = 8

// Picker is the fzf-style two-stage selector: filter providers → filter that
// provider's models. Looks and types like the `/` palette.
type Picker struct {
	cfg    config.Config
	styles Styles
	lister ModelLister
	width  int
	focus  pickerColumn
	active bool

	providers        []providerItem
	modelsByProvider map[string][]modelItem

	filter   textinput.Model
	provQuery string
	modelQuery string
	provSel  int
	modelSel int

	loading map[string]bool
	loadErr map[string]error
	spin    spinner.Model

	currentProvider string
}

// NewPicker builds the picker in its inactive state.
func NewPicker(cfg config.Config) *Picker {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(colorHoney)
	ti := textinput.New()
	ti.Placeholder = "type to filter…"
	ti.Prompt = "› "
	ti.CharLimit = 64
	ti.Focus()
	return &Picker{
		cfg:              cfg,
		styles:           DefaultStyles(),
		lister:           defaultLister,
		focus:            colProviders,
		providers:        buildProviderList(cfg),
		modelsByProvider: map[string][]modelItem{},
		filter:           ti,
		loading:          map[string]bool{},
		loadErr:          map[string]error{},
		spin:             sp,
	}
}

// SetLister overrides the model-fetch function. Tests use this.
func (p *Picker) SetLister(l ModelLister) { p.lister = l }

// Active reports whether the picker is open.
func (p *Picker) Active() bool { return p.active }

// Show opens the picker on the provider stage and kicks off an async load
// for the highlighted provider so models are ready by the time the user
// advances to stage 2.
func (p *Picker) Show() tea.Cmd {
	p.active = true
	p.focus = colProviders
	p.provQuery = ""
	p.modelQuery = ""
	p.provSel = 0
	p.modelSel = 0
	p.filter.SetValue("")
	p.filter.Focus()
	return p.loadCurrentProvider()
}

// Hide closes the picker without emitting a PickedMsg.
func (p *Picker) Hide() { p.active = false }

// SetSize records the available width so rows can truncate cleanly.
func (p *Picker) SetSize(w, _ int) { p.width = w }

// Update handles key events + async messages.
func (p *Picker) Update(msg tea.Msg) (*Picker, tea.Cmd) {
	if !p.active {
		return p, nil
	}
	switch m := msg.(type) {
	case modelsLoadedMsg:
		p.loading[m.provider] = false
		if m.err != nil {
			p.loadErr[m.provider] = m.err
		} else {
			p.loadErr[m.provider] = nil
			p.modelsByProvider[m.provider] = modelEntries(m.models)
			if m.provider == p.currentProvider {
				p.modelSel = 0
			}
		}
		return p, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		p.spin, cmd = p.spin.Update(m)
		return p, cmd
	case tea.KeyMsg:
		return p.handleKey(m)
	}
	return p, nil
}

func (p *Picker) handleKey(km tea.KeyMsg) (*Picker, tea.Cmd) {
	switch km.String() {
	case "esc", "ctrl+c":
		// ctrl+c collapses both stages in one press so user is never stuck —
		// esc-from-models normally just goes back to providers, but if a
		// provider load failed (e.g. chatgpt 401 before /login) the bounce
		// back puts them right back into the same broken stage.
		if km.String() == "esc" && p.focus == colModels {
			p.focus = colProviders
			p.filter.SetValue(p.provQuery)
			p.filter.CursorEnd()
			return p, nil
		}
		p.active = false
		return p, func() tea.Msg { return PickerDismissedMsg{} }
	case "enter":
		return p.advance()
	case "down", "ctrl+n":
		p.move(1)
		return p, nil
	case "up", "ctrl+p":
		p.move(-1)
		return p, nil
	case "ctrl+r":
		if p.focus == colModels && p.currentProvider != "" {
			delete(p.modelsByProvider, p.currentProvider)
			return p, p.loadProvider(p.currentProvider)
		}
	case "ctrl+l":
		// auth-error escape hatch: open LoginPane for the failing provider.
		// Bound on ctrl+l (not plain l) so the letter still types into the
		// filter input. The main app handles the message via PickerLoginRequestedMsg.
		if p.focus == colModels && p.currentProvider != "" {
			if err := p.loadErr[p.currentProvider]; err != nil && isAuthErr(err.Error()) {
				name := p.currentProvider
				p.active = false
				return p, func() tea.Msg { return PickerLoginRequestedMsg{Provider: name} }
			}
		}
	}

	prev := p.filter.Value()
	var cmd tea.Cmd
	p.filter, cmd = p.filter.Update(km)
	if p.filter.Value() != prev {
		if p.focus == colProviders {
			p.provQuery = p.filter.Value()
			p.provSel = 0
		} else {
			p.modelQuery = p.filter.Value()
			p.modelSel = 0
		}
	}
	return p, cmd
}

// move shifts the selection in the active column by delta, clamped to the
// visible (post-filter) range.
func (p *Picker) move(delta int) {
	matches := p.activeMatches()
	if len(matches) == 0 {
		return
	}
	sel := &p.provSel
	if p.focus == colModels {
		sel = &p.modelSel
	}
	*sel += delta
	if *sel < 0 {
		*sel = 0
	}
	if *sel >= len(matches) {
		*sel = len(matches) - 1
	}
}

// advance handles enter: stage 1 → load + jump to stage 2; stage 2 → commit.
func (p *Picker) advance() (*Picker, tea.Cmd) {
	if p.focus == colProviders {
		name := p.selectedProvider()
		if name == "" {
			return p, nil
		}
		p.currentProvider = name
		p.focus = colModels
		p.filter.SetValue(p.modelQuery)
		p.filter.CursorEnd()
		return p, p.loadProvider(name)
	}
	prov := p.currentProvider
	mid := p.selectedModelID()
	if prov == "" || mid == "" {
		return p, nil
	}
	p.active = false
	return p, func() tea.Msg { return PickedMsg{Provider: prov, Model: mid} }
}

// providerSelectedName returns the highlighted provider's name. Kept for
// backward compatibility with the test suite + app.go callers.
func (p *Picker) providerSelectedName() string { return p.selectedProvider() }

func (p *Picker) selectedProvider() string {
	matches := p.matchProviders()
	if p.provSel < 0 || p.provSel >= len(matches) {
		return ""
	}
	return p.providers[matches[p.provSel].Index].name
}

func (p *Picker) selectedModelID() string {
	matches := p.matchModels()
	if p.modelSel < 0 || p.modelSel >= len(matches) {
		return ""
	}
	return p.modelsByProvider[p.currentProvider][matches[p.modelSel].Index].id
}

// loadCurrentProvider kicks off a fetch for whichever provider is highlighted.
func (p *Picker) loadCurrentProvider() tea.Cmd {
	name := p.selectedProvider()
	if name == "" {
		return nil
	}
	p.currentProvider = name
	return p.loadProvider(name)
}

// loadProvider starts an async fetch unless we already have it cached.
func (p *Picker) loadProvider(name string) tea.Cmd {
	if _, ok := p.modelsByProvider[name]; ok {
		return nil
	}
	if p.loading[name] {
		return p.spin.Tick
	}
	p.loading[name] = true
	cfg := p.cfg.Providers[name]
	lister := p.lister
	return tea.Batch(
		p.spin.Tick,
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			models, err := lister(ctx, name, cfg)
			return modelsLoadedMsg{provider: name, models: models, err: err}
		},
	)
}

// activeMatches returns the current column's fuzzy-filtered match slice.
func (p *Picker) activeMatches() []fuzzy.Match {
	if p.focus == colProviders {
		return p.matchProviders()
	}
	return p.matchModels()
}

func (p *Picker) matchProviders() []fuzzy.Match {
	return fuzzyAll(p.provQuery, p.providers, func(i int) string {
		return p.providers[i].name + " " + p.providers[i].cfg.BaseURL
	})
}

func (p *Picker) matchModels() []fuzzy.Match {
	models := p.modelsByProvider[p.currentProvider]
	return fuzzyAll(p.modelQuery, models, func(i int) string {
		return models[i].display + " " + models[i].id + " " + models[i].desc
	})
}

// helpers (fuzzyAll, buildProviderList, modelEntries) live in picker_items.go.
