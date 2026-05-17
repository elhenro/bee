package prompt

import (
	"os"
	"path/filepath"
)

// ContextFile is one loaded AGENTS.md/CLAUDE.md.
type ContextFile struct {
	Path string
	Body string
}

// candidateNames are filenames bee searches for at each dir level.
// if both present at same dir, AGENTS.md wins.
var candidateNames = []string{"AGENTS.md", "CLAUDE.md"}

// LoadContextFiles walks from cwd up to filesystem root, collecting any
// AGENTS.md or CLAUDE.md found. Returns shallowest first (root) to
// deepest last (cwd). If beeHome is non-empty, it is checked for a
// global AGENTS.md and prepended.
func LoadContextFiles(cwd, beeHome string) []ContextFile {
	var out []ContextFile

	if beeHome != "" {
		if cf, ok := tryLoad(beeHome); ok {
			out = append(out, cf)
		}
	}

	var dirs []string
	cur, err := filepath.Abs(cwd)
	if err != nil {
		return out
	}
	for {
		dirs = append([]string{cur}, dirs...)
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	for _, d := range dirs {
		if cf, ok := tryLoad(d); ok {
			out = append(out, cf)
		}
	}
	return out
}

func tryLoad(dir string) (ContextFile, bool) {
	for _, name := range candidateNames {
		p := filepath.Join(dir, name)
		b, err := os.ReadFile(p)
		if err == nil {
			abs, _ := filepath.Abs(p)
			visited := map[string]bool{abs: true}
			body := expandContextImports(string(b), dir, visited, 0)
			return ContextFile{Path: p, Body: body}, true
		}
	}
	return ContextFile{}, false
}

// maxContextFileBytes caps each loaded context file (post-import-expansion)
// so a chatty CLAUDE.md doesn't blow the system-prompt budget. ~8192 chars
// ≈ 2K tokens — gives @imports headroom while the per-profile
// SystemPromptBudget acts as the real backstop.
const maxContextFileBytes = 8192

// renderContextSection formats loaded files into one prompt block.
// returns empty string if no files.
func renderContextSection(files []ContextFile) string {
	if len(files) == 0 {
		return ""
	}
	s := "## Project Context\n"
	for _, f := range files {
		body := f.Body
		if len(body) > maxContextFileBytes {
			body = body[:maxContextFileBytes] + "\n…[truncated, use view to read full]"
		}
		s += "<context path=\"" + f.Path + "\">\n" + body + "\n</context>\n"
	}
	return s
}
