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
		Description: "ALWAYS use `search` for file content search. NEVER invoke `grep`, `rg`, or `ack` via the `bash` tool — shell variants miss bee's project-aware excludes (.claude, vendor, testdata) and inflate counts with worktree duplicates. " +
			"Regex (Go RE2 syntax) over file contents. Returns up to 200 matches as path:line:text (match) or path:line-text (context). " +
			"ANCHOR your pattern: `^func Test` not `func Test` — unanchored patterns match comments, strings, fixtures and inflate counts. " +
			"For counting (e.g. 'how many tests'), use count_only=true to get per-file counts; an outlier file reveals fixtures/generated code skewing totals. " +
			"Args: pattern (required), path (dir), glob (bare ext like 'go' OR shell glob like '*.go', '*_test.go'), context (int, 0-5 surrounding lines per match), count_only (bool, per-file match counts only).",
		PromptSnippet: "search file contents by regex (use this, NOT shell `grep`)",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":    map[string]any{"type": "string"},
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
	root, _ := in["path"].(string)
	if root == "" {
		root = t.root
	}
	glob, _ := in["glob"].(string)
	ctxLines := tools.IntArg(in, "context", 0)
	if ctxLines < 0 {
		ctxLines = 0
	}
	if ctxLines > 5 {
		ctxLines = 5
	}
	countOnly, _ := in["count_only"].(bool)

	var out []string
	count := 0
	walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			name := d.Name()
			// .claude holds agent state (worktrees with duplicate code) — skip
			// or counts/searches double-count every file.
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "testdata" || name == ".claude" {
				return filepath.SkipDir
			}
			return nil
		}
		if glob != "" && !matchGlob(glob, p) {
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
		if countOnly {
			n := countMatches(f, re)
			if n > 0 {
				out = append(out, fmt.Sprintf("%s:%d", rel, n))
				count++
				if count >= t.max {
					return filepath.SkipAll
				}
			}
			return nil
		}
		if ctxLines == 0 {
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
			ln := 0
			for sc.Scan() {
				ln++
				line := sc.Text()
				if re.MatchString(line) {
					if count >= t.max {
						return filepath.SkipAll
					}
					out = append(out, fmt.Sprintf("%s:%d:%s", rel, ln, line))
					count++
				}
			}
			return nil
		}
		emitted := scanWithContext(f, re, rel, ctxLines, t.max-count)
		out = append(out, emitted...)
		count += len(emitted)
		if count >= t.max {
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil && walkErr != filepath.SkipAll {
		return tools.Result{Content: walkErr.Error(), IsError: true}, nil
	}
	if len(out) == 0 {
		return tools.Result{Content: "no matches"}, nil
	}
	return tools.Result{Content: strings.Join(out, "\n")}, nil
}

// countMatches returns the number of matching lines in f.
func countMatches(f *os.File, re *regexp.Regexp) int {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	n := 0
	for sc.Scan() {
		if re.Match(sc.Bytes()) {
			n++
		}
	}
	return n
}

// scanWithContext emits up to budget result strings with ctxN surrounding
// lines per match. Match lines use ':' separator; context lines use '-'.
// Adjacent windows merge naturally because each line is emitted at most once.
func scanWithContext(f *os.File, re *regexp.Regexp, path string, ctxN, budget int) []string {
	if budget <= 0 {
		return nil
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	emitted := make(map[int]bool, 16)
	var out []string
	for i, line := range lines {
		if !re.MatchString(line) {
			continue
		}
		start := i - ctxN
		if start < 0 {
			start = 0
		}
		end := i + ctxN
		if end >= len(lines) {
			end = len(lines) - 1
		}
		for j := start; j <= end; j++ {
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
