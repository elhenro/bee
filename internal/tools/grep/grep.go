// Package grep implements the grep tool: recursive regex search.
package grep

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/safety"
	"github.com/elhenro/bee/internal/tools"
)

// isBinary peeks the first 8KB. NUL byte = binary (compiled exe, image,
// archive). Seeks back to 0 so the scanner reads from the start. On read
// error returns false (skip-on-error is the walker's job, not ours).
func isBinary(f *os.File) bool {
	var buf [8192]byte
	n, err := f.Read(buf[:])
	if err != nil && err != io.EOF {
		return false
	}
	binary := bytes.IndexByte(buf[:n], 0) >= 0
	_, _ = f.Seek(0, io.SeekStart)
	return binary
}

const toolName = "search"

// matchGlob filters file paths against the user-supplied glob arg. Accepts
// three shapes so the model can't trip over its own convention drift:
//   - bare extension ("go", "ts")       → suffix match on "."+ext
//   - shell-style glob ("*.go", "*_test.go", "*.test.*") → filepath.Match on basename
//   - leading-dot bare ext (".go")      → suffix match on ext
//
// Any unparseable pattern returns false (skip file) rather than erroring the
// whole walk — bad glob just narrows results.
func matchGlob(glob, path string) bool {
	if glob == "" {
		return true
	}
	if strings.ContainsAny(glob, "*?[") {
		ok, err := filepath.Match(glob, filepath.Base(path))
		return err == nil && ok
	}
	ext := glob
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return strings.HasSuffix(path, ext)
}

// Tool is the grep tool.
type Tool struct {
	root string
	max  int
}

const defaultMax = 200

// New returns a grep tool rooted at root with the default match cap (200).
func New(root string) *Tool { return &Tool{root: root, max: defaultMax} }

// NewWithMax returns a grep tool with a custom match cap. Tiny profile uses
// 50 so a single grep can't dump a wall of paths into a 4-8k context.
func NewWithMax(root string, n int) *Tool {
	if n <= 0 {
		n = defaultMax
	}
	return &Tool{root: root, max: n}
}

// Spec advertises the tool to the model.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: toolName,
		Description: "Regex (Go RE2 syntax) over file contents. Auto-excludes .claude, vendor, testdata and dedupes worktrees. Returns up to 200 matches as path:line:text (match) or path:line-text (context). " +
			"ANCHOR your pattern: `^func Test` not `func Test` — unanchored patterns match comments, strings, fixtures and inflate counts. " +
			"For counting (e.g. 'how many tests'), use count_only=true to get per-file counts; an outlier file reveals fixtures/generated code skewing totals. " +
			"Args: pattern (required), path (dir), glob (bare ext like 'go' OR shell glob like '*.go', '*_test.go'), context (int, 0-5 surrounding lines per match), count_only (bool, per-file match counts only).",
		PromptSnippet: "search file contents by regex",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":    map[string]any{"type": "string", "minLength": 1},
				"path":       map[string]any{"type": "string"},
				"glob":       map[string]any{"type": "string"},
				"context":    map[string]any{"type": "integer", "description": "Surrounding lines per match (clamped 0..5). Match lines use ':' separator; context lines use '-'."},
				"count_only": map[string]any{"type": "boolean", "description": "Return only per-file match counts (path:count). Cheap survey of where matches live."},
			},
			"required": []string{"pattern"},
		},
	}
}

// Run executes the search.
func (t *Tool) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	pat, _ := in["pattern"].(string)
	if pat == "" {
		return tools.Result{Content: "missing pattern", IsError: true}, nil
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return tools.Result{Content: "bad regex: " + err.Error(), IsError: true}, nil
	}
	reqPath, _ := in["path"].(string)
	root, err := tools.ResolveSearchRoot(t.root, reqPath)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	glob, _ := in["glob"].(string)
	ctxLines := min(max(tools.IntArg(in, "context", 0), 0), 5)
	countOnly, _ := in["count_only"].(bool)

	var out []string
	err = tools.WalkFiles(ctx, root, func(p string) error {
		if glob != "" && !matchGlob(glob, p) {
			return nil
		}
		// under a confined scope, skip secret-shaped files (id_rsa, .env,
		// credentials…) so search can't return their contents — matches the read
		// tool's CheckReadable guard. danger-full-access reads them like read does.
		if t.root != "" && safety.CheckReadable(p) != nil {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer f.Close()
		// Sniff first 8KB for NUL byte → treat as binary, skip. Stops grep
		// walking compiled binaries (./bee), images, and other blob content
		// whose "lines" are mostly control bytes — those wreck the TUI
		// preview with embedded \r/\x00/etc.
		if isBinary(f) {
			return nil
		}
		rel := tools.RelTo(root, p)
		switch {
		case countOnly:
			n := 0
			eachLine(f, func(_ int, line []byte) bool {
				if re.Match(line) {
					n++
				}
				return true
			})
			if n > 0 {
				out = append(out, fmt.Sprintf("%s:%d", rel, n))
			}
		case ctxLines == 0:
			eachLine(f, func(ln int, line []byte) bool {
				if re.Match(line) {
					out = append(out, fmt.Sprintf("%s:%d:%s", rel, ln, line))
				}
				return len(out) < t.max
			})
		default:
			out = append(out, scanWithContext(f, re, rel, ctxLines, t.max-len(out))...)
		}
		if len(out) >= t.max {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	return tools.LinesResult(out), nil
}

// eachLine calls fn with every line of f and its 1-based number until fn
// returns false. A scan error just ends the file.
func eachLine(f *os.File, fn func(n int, line []byte) bool) {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for n := 1; sc.Scan(); n++ {
		if !fn(n, sc.Bytes()) {
			return
		}
	}
}

// scanWithContext emits up to budget result strings with ctxN surrounding
// lines per match. Match lines use ':' separator; context lines use '-'.
// Adjacent windows merge naturally because each line is emitted at most once.
func scanWithContext(f *os.File, re *regexp.Regexp, path string, ctxN, budget int) []string {
	if budget <= 0 {
		return nil
	}
	var lines []string
	eachLine(f, func(_ int, line []byte) bool {
		lines = append(lines, string(line))
		return true
	})
	emitted := make(map[int]bool, 16)
	var out []string
	for i, line := range lines {
		if !re.MatchString(line) {
			continue
		}
		for j := max(i-ctxN, 0); j <= min(i+ctxN, len(lines)-1); j++ {
			if emitted[j] {
				continue
			}
			emitted[j] = true
			sep := "-"
			if j == i {
				sep = ":"
			}
			out = append(out, fmt.Sprintf("%s:%d%s%s", path, j+1, sep, lines[j]))
			if len(out) >= budget {
				return out
			}
		}
	}
	return out
}
