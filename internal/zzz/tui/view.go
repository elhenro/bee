package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// View renders the whole TUI. Layout (top→bottom):
//
//	header     — bee glyph + run id + branch + status
//	timeline   — moon-phase iteration ledger (most-recent at the bottom)
//	log        — operator notes, warnings, Drive's Println() lines
//	live       — current iter + phase + tokens + elapsed
//	bee        — animated sleeping-bee ASCII (always last decorative row)
//	input      — textarea (steering)
//	footer     — keybinding hint
func (m *Model) View() string {
	if m.width == 0 {
		return styDim.Render("starting zzz…")
	}
	innerW := m.width - 2
	if innerW < 40 {
		innerW = 40
	}

	var b strings.Builder
	b.WriteString(m.header(innerW))
	b.WriteString("\n")
	b.WriteString(m.timeline(innerW))
	b.WriteString("\n")
	b.WriteString(m.logPanel(innerW))
	b.WriteString("\n")
	b.WriteString(m.livePanel(innerW))
	b.WriteString("\n")
	b.WriteString(m.beePanel(innerW))
	b.WriteString("\n")
	if m.done {
		b.WriteString(m.finalPanel(innerW))
		b.WriteString("\n")
		b.WriteString(styDim.Render("  press q or ctrl+d to exit"))
		return b.String()
	}
	b.WriteString(m.input.View())
	b.WriteString("\n")
	b.WriteString(styDim.Render("  enter to send · /stop · /abort · /note <text> · ctrl+c = graceful stop"))
	return b.String()
}

func (m *Model) header(w int) string {
	left := styHoney.Render("⬢ bee zzz") + "  " +
		stySmoke.Render(m.run.ID) + "  " +
		styDim.Render("· "+m.run.Branch+" ·") + " " +
		styBee.Render(m.run.Status)
	right := styDim.Render(time.Since(m.start).Truncate(time.Second).String())
	return padBetween(left, right, w)
}

// timeline renders the iteration ledger. Each row: moon + iter# + phase +
// tokens. Constrained to the last N rows so the panel doesn't push the bee
// off-screen on long runs.
func (m *Model) timeline(w int) string {
	const maxRows = 6
	if len(m.rows) == 0 {
		return styDim.Render("  no iterations yet…")
	}
	rows := m.rows
	if len(rows) > maxRows {
		rows = rows[len(rows)-maxRows:]
	}
	var lines []string
	lines = append(lines, stySmoke.Render("  timeline"))
	for _, r := range rows {
		glyph := phaseGlyph(r.status)
		idx := stySmoke.Render(fmt.Sprintf("iter %02d", r.n))
		tok := styDim.Render(fmt.Sprintf("%dk in / %dk out", r.tokens.Input/1000, r.tokens.Output/1000))
		stat := phaseColor(r.status).Render(r.status)
		line := fmt.Sprintf("  %s  %s  %s  %s", glyph, idx, stat, tok)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) logPanel(w int) string {
	const maxLines = 6
	if len(m.log) == 0 {
		return styDim.Render("  log · (idle)")
	}
	lines := m.log
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	var b strings.Builder
	b.WriteString(stySmoke.Render("  log"))
	for _, l := range lines {
		b.WriteString("\n  ")
		switch l.level {
		case "warn":
			b.WriteString(styWarning.Render(l.text))
		case "err":
			b.WriteString(styError.Render(l.text))
		default:
			b.WriteString(styBody.Render(l.text))
		}
	}
	return b.String()
}

func (m *Model) livePanel(w int) string {
	elapsed := time.Since(m.start).Truncate(time.Second)
	phase := m.phase
	if phase == "" {
		phase = "—"
	}
	iter := fmt.Sprintf("iter %d/%d", m.iter, m.maxIt)
	tok := fmt.Sprintf("%d in / %d out  $%.4f", m.tokens.Input, m.tokens.Output, m.tokens.USD)
	left := styBee.Render("●") + " " + styBright.Render(iter) + "  " +
		styTool.Render("phase="+phase) + "  " + styDim.Render(tok)
	right := styDim.Render("t+" + elapsed.String())
	return padBetween(left, right, w)
}

func (m *Model) beePanel(w int) string {
	top, mid, bot := BeeFrame(m.tick)
	style := lipgloss.NewStyle().Foreground(accentBee)
	dim := styDim
	var b strings.Builder
	b.WriteString(dim.Render(top))
	b.WriteString("\n")
	b.WriteString(style.Render(mid))
	b.WriteString("\n")
	b.WriteString(dim.Render(bot))
	return b.String()
}

func (m *Model) finalPanel(w int) string {
	r := m.final
	if r == nil {
		r = m.run
	}
	dur := r.EndedAt.Sub(r.StartedAt).Truncate(time.Second)
	if r.EndedAt.IsZero() {
		dur = time.Since(r.StartedAt).Truncate(time.Second)
	}
	var b strings.Builder
	b.WriteString(styHoney.Render("══ zzz summary ══") + "\n")
	b.WriteString(stySmoke.Render(fmt.Sprintf("  status   : %s", r.Status)) + "\n")
	b.WriteString(stySmoke.Render(fmt.Sprintf("  cause    : %s", r.StopCause)) + "\n")
	b.WriteString(stySmoke.Render(fmt.Sprintf("  iters    : %d", r.IterCount)) + "\n")
	b.WriteString(stySmoke.Render(fmt.Sprintf("  commits  : %d", len(r.Commits))) + "\n")
	b.WriteString(stySmoke.Render(fmt.Sprintf("  tokens   : %d in / %d out  $%.4f",
		r.Tokens.Input, r.Tokens.Output, r.Tokens.USD)) + "\n")
	b.WriteString(stySmoke.Render(fmt.Sprintf("  duration : %s", dur)) + "\n")
	b.WriteString(stySmoke.Render("  inspect  : ~/.bee/zzz/runs/"+r.ID+"/"))
	if m.finalEr != nil {
		b.WriteString("\n")
		b.WriteString(styError.Render("  error    : " + m.finalEr.Error()))
	}
	return b.String()
}

// padBetween renders left and right separated by enough whitespace to fill w.
// If the combined width already exceeds w it falls back to "left  right".
func padBetween(left, right string, w int) string {
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	gap := w - lw - rw
	if gap < 2 {
		gap = 2
	}
	return left + strings.Repeat(" ", gap) + right
}
