package agents

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/elhenro/bee/internal/bgreg"
)

// sectionKind groups rows for display.
type sectionKind int

const (
	secErrors sectionKind = iota
	secNeedsInput
	secRunning
	secDoneUnmerged
	secMerged
)

func (k sectionKind) title() string {
	switch k {
	case secErrors:
		return "ERRORS"
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
	rows []bgreg.Status
}

func classify(s bgreg.Status) sectionKind {
	switch {
	case s.State == bgreg.StateFailed:
		return secErrors
	case s.MergeState == bgreg.MergeStateConflict, s.State == bgreg.StateAwaiting:
		return secNeedsInput
	case s.MergeState == bgreg.MergeStateMerged:
		return secMerged
	case s.State == bgreg.StateDone:
		return secDoneUnmerged
	}
	return secRunning
}

// buildSections groups statuses into the four fixed sections in render order.
func buildSections(all []bgreg.Status) []section {
	groups := map[sectionKind][]bgreg.Status{}
	for _, s := range all {
		k := classify(s)
		groups[k] = append(groups[k], s)
	}
	var out []section
	for _, k := range []sectionKind{secErrors, secNeedsInput, secRunning, secDoneUnmerged, secMerged} {
		if len(groups[k]) > 0 {
			out = append(out, section{kind: k, rows: groups[k]})
		}
	}
	return out
}

// flatten returns rows in render order alongside their absolute index, so
// arrow-key navigation can address them via a single int.
func flatten(secs []section) []bgreg.Status {
	var out []bgreg.Status
	for _, s := range secs {
		out = append(out, s.rows...)
	}
	return out
}

// renderHeader is the top status line: counts + pending model.
func renderHeader(all []bgreg.Status, pendingModel, pendingProvider string, prefs Prefs) string {
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
	if !prefs.ShowSubheader {
		return header
	}

	modelLabel := pendingModel
	if modelLabel == "" {
		modelLabel = "default"
	}
	provLabel := pendingProvider
	if provLabel == "" {
		provLabel = "default"
	}
	sub := subtitleStyle.Render(fmt.Sprintf("next spawn → %s/%s", provLabel, modelLabel))
	return header + "\n" + sub
}

// sectionHint returns a parenthetical suffix describing what the user should
// expect for this section. Empty when no useful hint applies (e.g. RUNNING,
// MERGED, or a DONE-UNMERGED section that holds only failed agents — those
// won't merge so the "auto-merging…" line would mislead).
func sectionHint(sec section) string {
	switch sec.kind {
	case secDoneUnmerged:
		if slices.ContainsFunc(sec.rows, func(r bgreg.Status) bool { return r.State == bgreg.StateDone }) {
			return "   (auto-merging…)"
		}
	case secNeedsInput:
		if slices.ContainsFunc(sec.rows, func(r bgreg.Status) bool { return r.MergeState == bgreg.MergeStateConflict }) {
			return "   (open agent to resolve conflict)"
		}
		return "   (open agent to reply)"
	}
	return ""
}

// renderRow formats one agent into a status line (and optional peek line).
func renderRow(r bgreg.Status, selected bool, width int, prefs Prefs) string {
	marker := "  "
	if selected {
		marker = cursorStyle.Render("❯ ")
	}
	state := stateGlyph(r)
	tw := taskWidth(width)
	task := truncate(r.Task, tw)
	elapsed := humanDuration(r.StartedAt, r.FinishedAt)
	tokens := fmt.Sprintf("↑%s ↓%s", humanK(r.InputTokens), humanK(r.OutputTokens))

	line1 := fmt.Sprintf("%s%s %-*s  %-9s  %-12s", marker, state, tw, task, elapsed, tokens)
	if prefs.ShowChip {
		model := shortModel(r.Model)
		line1 += "  " + modelChipStyle.Render("["+model+"]")
	}

	if prefs.ShowBadges {
		badge := ""
		switch r.MergeState {
		case bgreg.MergeStateConflict:
			badge = badStyle.Render("⚠ conflict")
		case bgreg.MergeStateBlocked:
			badge = badStyle.Render("⛔ blocked")
		case bgreg.MergeStateMerged:
			badge = goodStyle.Render("✓ merged")
		case bgreg.MergeStateMerging:
			badge = busyStyle.Render("⤴ merging")
		case bgreg.MergeStateUnmerged:
			if r.State == bgreg.StateDone {
				badge = warnStyle.Render("⚠ unmerged")
			}
		}
		if badge != "" {
			line1 += "  " + badge
		}
	}

	if !prefs.ShowPeek {
		return line1
	}

	peek := strings.TrimSpace(r.LastResponse)
	// a blocked agent's last reply is stale; the reason is what needs a human.
	if r.MergeState == bgreg.MergeStateBlocked && r.ConflictMsg != "" {
		peek = "blocked: " + r.ConflictMsg
	}
	if peek == "" {
		peek = dimStyle.Render("(no response yet)")
	} else {
		peek = "    " + dimStyle.Render("⎿  "+truncateLines(peek, width-10))
	}
	return line1 + "\n" + peek
}

// task column: what's left of the fixed columns (state, time, tokens, model
// chip, badge), clamped so a wide terminal doesn't strand the numbers far
// from the title and a narrow one still shows a few words
func taskWidth(width int) int {
	if width <= 0 {
		return 36
	}
	return min(max(width-66, 24), 56)
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

// humanDuration formats start→finish as a compact string. finish zero means
// the agent is still running, so we use time.Now() as the end.
func humanDuration(start, finish time.Time) string {
	if start.IsZero() {
		return "—"
	}
	end := finish
	if end.IsZero() {
		end = time.Now()
	}
	d := max(end.Sub(start), 0)
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
	return ansi.Truncate(m, 18, "…")
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if n <= 0 {
		return ""
	}
	return ansi.Truncate(s, n, "…")
}

// truncateLines clips s to a single line of at most width runes (multi-line
// inputs are collapsed).
func truncateLines(s string, width int) string {
	s = strings.ReplaceAll(s, "\n", " ⏎ ")
	if width < 10 {
		width = 80
	}
	return ansi.Truncate(s, width, "…")
}
