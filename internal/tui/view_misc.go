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
// Each row is colored via a honey gradient (bright honey → soft amber →
// quiet squid) so the braille pixel art reads as molten gold rather than
// flat dim text. Empty before width is known or after animation ends.
func (m Model) renderIntro() string {
	if !m.introActive || len(m.introFrames) == 0 {
		return ""
	}
	if m.introIdx < 0 || m.introIdx >= len(m.introFrames) {
		return ""
	}
	f := m.introFrames[m.introIdx]
	// gradient palette top→bottom — top sits brightest, fades to subtle
	rowColors := []lipgloss.AdaptiveColor{accentHoney, accentBee, fgSquid, fgOyster}
	lines := strings.Split(f.Text, "\n")
	for i, ln := range lines {
		col := rowColors[len(rowColors)-1]
		if i < len(rowColors) {
			col = rowColors[i]
		}
		lines[i] = lipgloss.NewStyle().Foreground(col).Render(ln)
	}
	art := strings.Join(lines, "\n")
	if f.Subtitle == "" {
		return art
	}
	sub := lipgloss.NewStyle().Foreground(fgOyster).Italic(true).Render("  " + f.Subtitle)
	return art + "\n" + sub
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
