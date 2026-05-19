package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// osUserHome is a tiny indirection so tests can override $HOME for cwd
// prettifying without touching the global env.
var osUserHome = func() (string, error) { return os.UserHomeDir() }

// prettyCwd shortens $HOME to ~ for a tidier status line.
func prettyCwd(p string) string {
	if home, err := osUserHome(); err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// renderIntro draws the current frame of the non-blocking startup animation.
// Each row is colored via a honey gradient (bright honey to soft amber to
// quiet squid) so the braille pixel art reads as molten gold rather than
// flat dim text. When the animation finishes the space stays reserved and
// a static "🐝 bee v<version>" placeholder takes over so the live region
// doesn't snap shorter. Empty before width is known or when the banner is
// disabled entirely.
func (m Model) renderIntro() string {
	if m.introActive && len(m.introFrames) > 0 &&
		m.introIdx >= 0 && m.introIdx < len(m.introFrames) {
		return renderIntroFrame(m.introFrames[m.introIdx])
	}
	if m.introDone {
		return renderIntroPlaceholder(m.width, m.introDoneFrame)
	}
	return ""
}

func renderIntroFrame(f IntroFrame) string {
	// gradient palette top-bottom, top sits brightest, fades to subtle
	rowColors := []lipgloss.AdaptiveColor{accentHoney, accentBee, fgSquid, fgOyster}
	lines := strings.Split(f.Text, "\n")
	for i, ln := range lines {
		col := rowColors[len(rowColors)-1]
		if i < len(rowColors) {
			col = rowColors[i]
		}
		lines[i] = lipgloss.NewStyle().Foreground(col).Render(ln)
	}
	// trailing blank line keeps height stable with renderIntroPlaceholder
	return strings.Join(lines, "\n") + "\n"
}

// introPulseFrames is the post-intro tick budget for the two bold-flashes
// on "bee". 16 frames x 70ms ~= 1.1s. After it elapses the full placeholder
// block is pushed to scrollback, preserving intro height.
const introPulseFrames = 16

// renderIntroPlaceholder fills the same vertical slot a live frame uses,
// introArtRows of braille + 1 subtitle row, with a centered
// "🐝 bee v<x>" label so the live region doesn't snap shorter while the
// pulse plays. Once the pulse settles, the caller hands the same block to
// tea.Println so the banner becomes a permanent scrollback entry at the
// top of the conversation, and renderIntro stops returning anything.
func renderIntroPlaceholder(width, pulseFrame int) string {
	const rows = introArtRows + 1
	bold := pulseFrame >= introPulseFrames || (pulseFrame/4)%2 == 1
	mid := rows / 2
	out := make([]string, rows)
	out[mid] = buildIntroBannerLine(width, bold)
	return strings.Join(out, "\n")
}

// buildIntroBannerLine renders the centered "🐝 bee v<x>" label. "bee" is
// bold honey when bold==true (the settled/flash state), slim honey when
// false (the off-phase of the pulse). The version stays slim oyster.
// Centering uses an explicit cell count, emoji = 2 cells, ASCII = 1 each,
// because lipgloss.Width can under-count emoji on some runewidth configs,
// shifting the label visibly off-center.
func buildIntroBannerLine(width int, bold bool) string {
	beeStyle := lipgloss.NewStyle().Foreground(accentHoney)
	if bold {
		beeStyle = beeStyle.Bold(true)
	}
	bee := beeStyle.Render("bee")
	dim := lipgloss.NewStyle().Foreground(fgOyster)
	ver := dim.Render("v" + Version)
	label := "🐝 " + bee + " " + ver
	// visible cells: 🐝 (2) + " " + "bee" (3) + " " + "v<ver>" (1+len)
	visibleCells := 2 + 1 + 3 + 1 + 1 + len(Version)
	if suf := lifetimeTokensTrailer(); suf != "" {
		label += dim.Render(suf)
		// trailer is " " + ASCII digits/letters/dot, so byte len == cells.
		visibleCells += len(suf)
	}
	if width > visibleCells {
		label = strings.Repeat(" ", (width-visibleCells)/2) + label
	}
	return label
}

// renderWarning returns a tiny dim notice line for transient engine events
// (stream retry, watchdog stall). Empty when no warning is active.
func (m Model) renderWarning() string {
	if m.warning == "" {
		return ""
	}
	bee := lipgloss.NewStyle().Foreground(accentHoney).Render("◌")
	body := lipgloss.NewStyle().Foreground(semWarning).Italic(true).Render(m.warning)
	return bee + " " + body
}

// gitBranch returns the current branch name when cwd lives inside a git repo.
// Walks up looking for .git (handles worktree pointer files too) and reads
// HEAD. Returns the branch name from "ref: refs/heads/<name>", or a 7-char
// short sha when HEAD is detached. Empty string when cwd is not in a repo.
// Cheap enough to call per-render: two small file reads at most.
func gitBranch(cwd string) string {
	if cwd == "" {
		return ""
	}
	dir := cwd
	for i := 0; i < 32; i++ {
		gitPath := filepath.Join(dir, ".git")
		st, err := os.Stat(gitPath)
		if err == nil {
			gitDir := gitPath
			if !st.IsDir() {
				// worktree pointer: ".git" file with "gitdir: <path>"
				b, err := os.ReadFile(gitPath)
				if err != nil {
					return ""
				}
				line := strings.TrimSpace(string(b))
				if !strings.HasPrefix(line, "gitdir: ") {
					return ""
				}
				gd := strings.TrimPrefix(line, "gitdir: ")
				if !filepath.IsAbs(gd) {
					gd = filepath.Join(dir, gd)
				}
				gitDir = gd
			}
			head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
			if err != nil {
				return ""
			}
			s := strings.TrimSpace(string(head))
			if rest, ok := strings.CutPrefix(s, "ref: refs/heads/"); ok {
				return rest
			}
			if len(s) >= 7 {
				return s[:7]
			}
			return s
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
