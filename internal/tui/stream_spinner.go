package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// braille dots: one cell, smooth, reads as "working" in any font
var spinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// flavor words for the busy row, one per turn (from the loader seed) so
// the word holds still while the glyph turns
var spinnerVerbs = [...]string{
	"Foraging", "Buzzing", "Pollinating", "Humming",
	"Brewing", "Gathering", "Waggling", "Swarming",
}

const interruptHint = "esc to interrupt"

func (r *StreamRenderer) spinnerVerb() string {
	seed := r.loaderStats.Seed
	if seed < 0 {
		seed = -seed
	}
	return spinnerVerbs[seed%int64(len(spinnerVerbs))]
}

// busy row for a streaming turn:
//
//	⠹ Foraging…  12s · ↑ 3.4k · ↓ 340 · esc to interrupt
//
// only the glyph turns; with show_loader off it's the static bee mark
func (r *StreamRenderer) RenderSpinner(frame int, elapsed time.Duration) string {
	return r.RenderSpinnerLabel(frame, r.spinnerVerb()+"…", elapsed, interruptHint)
}

// a figure on the busy row; higher shed goes first when the row is too wide
type spinnerChip struct {
	text string
	shed int
}

// RenderSpinner with a custom label and hint (compaction, handoff). figures
// that don't fit are dropped whole, never cut mid-item
func (r *StreamRenderer) RenderSpinnerLabel(frame int, label string, elapsed time.Duration, hint string) string {
	if frame < 0 {
		frame = -frame
	}
	glyph := glyphBee
	if r.showLoader {
		glyph = spinnerFrames[frame%len(spinnerFrames)]
	}
	row := r.styles.Mark.Render(glyph) + " " + r.styles.Key.Render(label)

	var chips []spinnerChip
	if elapsed >= time.Second {
		chips = append(chips, spinnerChip{formatClock(elapsed), 0})
	}
	st := r.loaderStats
	if st.ShowIn && st.InTokens > 0 {
		chips = append(chips, spinnerChip{"↑ " + tokensHuman(st.InTokens), 2})
	}
	if st.ShowOut && st.OutTokens > 0 {
		chips = append(chips, spinnerChip{"↓ " + tokensHuman(st.OutTokens), 3})
	}
	if st.ShowRate && st.RateTokS >= 1 {
		chips = append(chips, spinnerChip{tokensHuman(int(st.RateTokS)) + " tok/s", 4})
	}
	if hint != "" {
		chips = append(chips, spinnerChip{hint, 1})
	}
	room := r.width - 2 - lipgloss.Width(row) - 2
	if meta := fitChips(chips, room); meta != "" {
		row += "  " + r.styles.Dim.Render(meta)
	}
	return r.gutter(clipLine(row, r.width-2))
}

// the chips joined for the row. they claim the room in keep order (lowest
// shed first) and one that doesn't fit is skipped, so a long chip can't cost
// the short ones behind it their place
func fitChips(chips []spinnerChip, room int) string {
	keep := make([]bool, len(chips))
	join := func() string {
		var texts []string
		for i, c := range chips {
			if keep[i] {
				texts = append(texts, c.text)
			}
		}
		return strings.Join(texts, " "+glyphSep+" ")
	}
	order := make([]int, len(chips))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return chips[order[a]].shed < chips[order[b]].shed })
	for _, i := range order {
		keep[i] = true
		if lipgloss.Width(join()) > room {
			keep[i] = false
		}
	}
	return join()
}
