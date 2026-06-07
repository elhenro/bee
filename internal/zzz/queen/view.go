package queen

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	styHoney = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	stySmoke = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styBody  = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styRun   = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	styOK    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styErr   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

func statusStyle(s string) lipgloss.Style {
	switch s {
	case "completed":
		return styOK
	case "failed":
		return styErr
	case "aborted":
		return styWarn
	case "running":
		return styRun
	default:
		return stySmoke
	}
}

// View renders the queen dashboard: header, one row per worker bee, footer.
func (m *Model) View() string {
	if m.width == 0 {
		return styDim.Render("starting queen…")
	}
	var b strings.Builder
	elapsed := time.Since(m.start).Truncate(time.Second)
	header := styHoney.Render("⬢ bee zzz queen") + "  " +
		stySmoke.Render(fmt.Sprintf("%d bees", len(m.workers))) + "  " +
		styDim.Render(fmt.Sprintf("%d/%d done", m.nDone, len(m.workers))) + "  " +
		styDim.Render("t+"+elapsed.String())
	b.WriteString(header)
	b.WriteString("\n\n")

	for _, w := range m.workers {
		b.WriteString(m.workerRow(w))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	var totIn, totOut int
	var totUSD float64
	for _, w := range m.workers {
		totIn += w.tokens.Input
		totOut += w.tokens.Output
		totUSD += w.tokens.USD
	}
	b.WriteString(styDim.Render(fmt.Sprintf("  total %d in / %d out  $%.4f", totIn, totOut, totUSD)))
	b.WriteString("\n")

	if m.allDone() {
		b.WriteString(styHoney.Render("  all bees finished") + styDim.Render(" · press q to exit"))
	} else if m.stopArmed {
		b.WriteString(styWarn.Render("  stop-all sent — ctrl+c again to force-cancel · ctrl+d quit"))
	} else {
		b.WriteString(styDim.Render("  ctrl+c stop-all (2× force) · ctrl+d quit"))
	}
	return b.String()
}

func (m *Model) workerRow(w *workerState) string {
	dot := statusStyle(w.status).Render("●")
	id := stySmoke.Render(fmt.Sprintf("bee %02d", w.idx+1))
	iter := styBody.Render(fmt.Sprintf("iter %d/%d", w.iter, w.maxIt))
	phase := styDim.Render("phase=" + dash(w.phase))
	tok := styDim.Render(fmt.Sprintf("%dk/%dk", w.tokens.Input/1000, w.tokens.Output/1000))
	commits := styDim.Render(fmt.Sprintf("%d✓", w.commits))
	stat := statusStyle(w.status).Render(w.status)
	head := fmt.Sprintf("  %s %s  %s  %s  %s  %s  %s",
		dot, id, iter, phase, tok, commits, stat)
	if w.last != "" {
		head += "\n      " + styDim.Render(truncate(w.last, m.lastWidth()))
	}
	return head
}

func (m *Model) lastWidth() int {
	w := m.width - 8
	if w < 20 {
		w = 20
	}
	if w > 120 {
		w = 120
	}
	return w
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
