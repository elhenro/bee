package agents

import (
	"fmt"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/bgreg"
)

// row is one agent in the list, slotted into a section.
type row struct {
	bgreg.Status
}

// sectionKind groups rows for display.
type sectionKind int

const (
	secNeedsInput sectionKind = iota
	secRunning
	secDoneUnmerged
	secMerged
)

func (k sectionKind) title() string {
	switch k {
	case secNeedsInput:
		return "NEEDS INPUT"
	case secRunning:
		return "RUNNING"
	case secDoneUnmerged:
		return "DONE — UNMERGED"
	case secMerged:
		return "MERGED"
	}
	return ""
}

// section is a slice of rows under a header. Order in the overall list is
// stable: needs-input first, then running, done-unmerged, merged.
type section struct {
	kind sectionKind
	rows []row
}

func classify(s bgreg.Status) sectionKind {
	if s.MergeState == bgreg.MergeStateConflict {
		return secNeedsInput
	}
	if s.State == bgreg.StateAwaiting {
		return secNeedsInput
	}
	if s.MergeState == bgreg.MergeStateMerged {
		return secMerged
	}
	if s.State == bgreg.StateDone {
		return secDoneUnmerged
	}
	if s.State == bgreg.StateFailed {
		return secDoneUnmerged
	}
	return secRunning
}

// buildSections groups statuses into the four fixed sections in render order.
func buildSections(all []bgreg.Status) []section {
	groups := map[sectionKind][]row{}
	for _, s := range all {
		k := classify(s)
		groups[k] = append(groups[k], row{s})
	}
	var out []section
	for _, k := range []sectionKind{secNeedsInput, secRunning, secDoneUnmerged, secMerged} {
		if len(groups[k]) > 0 {
			out = append(out, section{kind: k, rows: groups[k]})
		}
	}
	return out
}

// flatten returns rows in render order alongside their absolute index, so
// j/k navigation can address them via a single int.
func flatten(secs []section) []row {
	var out []row
	for _, s := range secs {
		out = append(out, s.rows...)
	}
	return out
}

// renderHeader is the top status line: counts + pending model.
func renderHeader(all []bgreg.Status, pendingModel, pendingProvider string) string {
	running, awaiting, done, unmerged := 0, 0, 0, 0
	for _, s := range all {
		switch s.State {
		case bgreg.StateActive:
			running++
		case bgreg.StateAwaiting:
			awaiting++
		case bgreg.StateDone, bgreg.StateFailed:
			done++
			if s.MergeState != bgreg.MergeStateMerged {
				unmerged++
			}
		}
		if s.MergeState == bgreg.MergeStateConflict {
			awaiting++
		}
	}
	count := fmt.Sprintf("%d running, %d needs input, %d done", running, awaiting, done)
	if unmerged > 0 {
		count += badStyle.Render(fmt.Sprintf(" (%d unmerged)", unmerged))
	}
	header := titleStyle.Render("⬢ bee agents") + dimStyle.Render(" — "+count)

	modelLabel := pendingModel
	if modelLabel == "" {
		modelLabel = "(default)"
	}
	provLabel := pendingProvider
	if provLabel == "" {
		provLabel = "(default)"
	}
	sub := subtitleStyle.Render(fmt.Sprintf("next spawn → model: %s · provider: %s    (change with /model or /provider)", modelLabel, provLabel))
	return header + "\n" + sub
}

// renderRow formats one agent into a 2-line block (status line + peek).
func renderRow(r row, selected bool, width int) string {
	marker := "  "
	if selected {
		marker = cursorStyle.Render("▸ ")
	}
	state := stateGlyph(r.Status)
	task := truncate(r.Status.Task, 36)
	elapsed := humanAge(r.Status.StartedAt)
	tokens := fmt.Sprintf("↑%s ↓%s", humanK(r.Status.InputTokens), humanK(r.Status.OutputTokens))
	model := shortModel(r.Status.Model)
	chip := modelChipStyle.Render("[" + model + "]")

	// merge state badge
	badge := ""
	switch r.Status.MergeState {
	case bgreg.MergeStateConflict:
		badge = badStyle.Render("⚠ conflict")
	case bgreg.MergeStateMerged:
		badge = goodStyle.Render("✓ merged")
	case bgreg.MergeStateMerging:
		badge = busyStyle.Render("⤴ merging")
	case bgreg.MergeStateUnmerged:
		if r.Status.State == bgreg.StateDone {
			badge = warnStyle.Render("⚠ unmerged")
		}
	}

	line1 := fmt.Sprintf("%s%s %-36s  %-9s  %-12s  %s", marker, state, task, elapsed, tokens, chip)
	if badge != "" {
		line1 += "  " + badge
	}

	// peek line
	peek := strings.TrimSpace(r.Status.LastResponse)
	if peek == "" {
		peek = dimStyle.Render("(no response yet)")
	} else {
		peek = "      › " + dimStyle.Render(truncateLines(peek, width-10, 1))
	}
	return line1 + "\n" + peek
}

// stateGlyph is the leading dot indicating live state.
func stateGlyph(s bgreg.Status) string {
	switch s.State {
	case bgreg.StateActive:
		return busyStyle.Render("●")
	case bgreg.StateAwaiting:
		return warnStyle.Render("◌")
	case bgreg.StateDone:
		return goodStyle.Render("◉")
	case bgreg.StateFailed:
		return badStyle.Render("✗")
	}
	return dimStyle.Render("·")
}

func humanAge(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%02ds", m, s)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func humanK(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 10000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%dk", n/1000)
}

func shortModel(m string) string {
	if m == "" {
		return "default"
	}
	// strip common provider prefixes for display
	for _, p := range []string{"anthropic/", "openai/", "google/", "deepseek/"} {
		if strings.HasPrefix(m, p) {
			m = m[len(p):]
			break
		}
	}
	if len(m) > 18 {
		return m[:17] + "…"
	}
	return m
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// truncateLines clips s to a single line of at most width runes (multi-line
// inputs are collapsed). Currently lines argument is reserved for future
// multi-line peek support.
func truncateLines(s string, width, lines int) string {
	_ = lines
	s = strings.ReplaceAll(s, "\n", " ⏎ ")
	if width < 10 {
		width = 80
	}
	if len([]rune(s)) <= width {
		return s
	}
	r := []rune(s)
	return string(r[:width-1]) + "…"
}
