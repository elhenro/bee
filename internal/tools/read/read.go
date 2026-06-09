// Package read implements the read tool: read file or list directory.
// One stat call branches on type. Binary files are refused with a clear msg.
package read

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/safety"
	"github.com/elhenro/bee/internal/tools"
)

const (
	toolName        = "read"
	defaultLimit    = 2000
	maxLimit        = 10000
	maxTail         = 10000
	binarySniffSize = 4096
	// directory-listing caps: tiny profile vs everyone else.
	dirEntriesTiny    = 200
	dirEntriesDefault = 1000
)

// cacheEntry marks a read already served this session, keyed by file identity
// + slice. Presence drives the soft "unchanged since your earlier read" note;
// key includes mtime+size so any edit invalidates the entry naturally.
type cacheEntry struct {
	key string
}

// Tool is the read tool. Holds a per-tool (= per-Engine, in practice) cache
// so repeated reads of unchanged files within one session return a stub
// instead of replaying the body. defaultLines/maxLines override the package
// defaults; zero falls back to the package constants.
type Tool struct {
	mu           sync.Mutex
	cache        map[string]*cacheEntry
	defaultLines int
	maxLines     int
	confine      bool // when true, apply CheckReadable secret-path blocking
}

// New returns a fresh read tool with package-default limits (2000 default,
// 10000 max) and secret-path blocking on. Used by non-tiny profiles.
func New() tools.Tool { return NewWithLimits(defaultLimit, maxLimit, true) }

// NewWithLimits returns a read tool with custom default/max line caps. Tiny
// profile passes 100/500 so one read can't torch a 4-8k local-model context.
// confine gates the CheckReadable secret-path guard: true under a confined
// sandbox scope, false under danger-full-access where read may open any file
// (including .git/.ssh), matching bee's default "read anything".
func NewWithLimits(def, max int, confine bool) tools.Tool {
	if def <= 0 {
		def = defaultLimit
	}
	if max <= 0 {
		max = maxLimit
	}
	return &Tool{cache: make(map[string]*cacheEntry), defaultLines: def, maxLines: max, confine: confine}
}

// Spec advertises the tool to the model. defaultLines/maxLines values are
// rendered into the limit description so tiny-profile models learn the
// actual ceiling (was hardcoded "Default 2000" — wrong for tiny, which
// caps at 500 with default 100).
func (t *Tool) Spec() llm.ToolSpec {
	defLines := t.defaultLines
	if defLines <= 0 {
		defLines = defaultLimit
	}
	maxLines := t.maxLines
	if maxLines <= 0 {
		maxLines = maxLimit
	}
	limitDesc := fmt.Sprintf("Max lines to return (file only). Default %d. Pass an explicit value up to %d to read more in one call instead of multiple offset-paged reads.", defLines, maxLines)
	return llm.ToolSpec{
		Name: toolName,
		Description: "Read a text file or list a directory. Output format: '<line> │ <content>' (separator is space-pipe-space, never a tab). " +
			"With hashline=true: '<line>#<TAG> │ <content>' for hashline_edit anchors. Use tail=N for the last N lines (log-style). " +
			"Content is verbatim — leading tabs are part of the content, not the separator.",
		PromptSnippet: "Read file contents",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"minLength":   1,
					"description": "Absolute or relative path to a file or directory.",
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "1-based starting line (file only). Default 1.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": limitDesc,
				},
				"tail": map[string]any{
					"type":        "integer",
					"description": "Return the last N lines (file only). Overrides offset/limit when > 0. Capped at 10000.",
				},
				"hashline": map[string]any{
					"type":        "boolean",
					"description": "When true, prefix each line with its LINE#ID anchor (e.g. 42#VK) for use with hashline_edit. Format: '<line>#<TAG> │ <content>'.",
				},
			},
			"required": []string{"path"},
		},
	}
}

// Run dispatches to file or directory handling.
func (t *Tool) Run(ctx context.Context, input map[string]any) (tools.Result, error) {
	path, ok := input["path"].(string)
	if !ok || path == "" {
		return tools.Result{Content: "missing or empty 'path' field", IsError: true}, nil
	}
	// under a confined scope, refuse secret-shaped files. resolve symlinks first
	// so a symlink can't bypass the lexical secret-file rules by pointing at e.g.
	// ~/.ssh/id_rsa; broken symlinks fall through to the original path so the
	// stat below reports the real error. danger-full-access skips this entirely
	// so the model can read .git, dotfiles, etc.
	if t.confine {
		checkPath := path
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			checkPath = resolved
		}
		if err := safety.CheckReadable(checkPath); err != nil {
			return tools.Result{Content: err.Error(), IsError: true}, nil
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("stat %s: %v", path, err), IsError: true}, nil
	}

	if info.IsDir() {
		out, err := listDir(path, t.dirEntryCap())
		if err != nil {
			return tools.Result{Content: err.Error(), IsError: true}, nil
		}
		return tools.Result{Content: out}, nil
	}

	defLines := t.defaultLines
	if defLines <= 0 {
		defLines = defaultLimit
	}
	maxLines := t.maxLines
	if maxLines <= 0 {
		maxLines = maxLimit
	}
	offset := tools.IntArg(input, "offset", 1)
	limit := tools.IntArg(input, "limit", defLines)
	tail := tools.IntArg(input, "tail", 0)
	if offset < 1 {
		offset = 1
	}
	if limit < 1 {
		limit = 1
	}
	if limit > maxLines {
		limit = maxLines
	}
	if tail < 0 {
		tail = 0
	}
	if tail > maxTail {
		tail = maxTail
	}
	hashline := boolArg(input, "hashline", false)

	// cache key: identity (path+mtime+size) + slice (offset+limit+tail+hashline).
	// any edit bumps mtime/size and invalidates the entry.
	cacheKey := fmt.Sprintf("%s|%d|%d|%d|%d|%d|%t",
		path, info.ModTime().UnixNano(), info.Size(), offset, limit, tail, hashline)
	out, err := readFile(path, offset, limit, tail, hashline)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	// soft hint: never withhold content. on a repeat read of an unchanged
	// slice, append a one-line note so the model knows it already saw this
	// (curbs reflexive re-reads) without ever starving it — a hard cache that
	// drops the body breaks once context is compacted and the model no longer
	// has the earlier read to fall back on.
	if t.seenBefore(cacheKey) {
		out += "\n\n(note: unchanged since your earlier read this session)"
	}
	return tools.Result{Content: out}, nil
}

// seenBefore records the key and reports whether it had already been read
// this session. The key embeds mtime+size, so any edit yields a fresh key
// (reported not-seen) — the note only fires on a truly unchanged slice.
func (t *Tool) seenBefore(key string) bool {
	if t == nil || t.cache == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.cache[key]; ok {
		return true
	}
	t.cache[key] = &cacheEntry{key: key}
	return false
}

func readFile(path string, offset, limit, tail int, hashline bool) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	// sniff for binary
	head := make([]byte, binarySniffSize)
	n, _ := f.Read(head)
	if isBinary(head[:n]) {
		return "", fmt.Errorf("refusing to read binary file %s (sniffed %d bytes)", path, n)
	}
	if _, err := f.Seek(0, 0); err != nil {
		return "", err
	}
	if tail > 0 {
		return readTail(f, path, tail, hashline)
	}
	return readSlice(f, path, offset, limit, hashline)
}

// dirEntryCap bounds how many directory entries a single read returns. A tiny
// profile (low maxLines) gets a tighter cap so `read .` on node_modules/.git
// can't flood a 4-8k local context; richer profiles get headroom.
func (t *Tool) dirEntryCap() int {
	if t.maxLines > 0 && t.maxLines <= 500 {
		return dirEntriesTiny
	}
	return dirEntriesDefault
}

func listDir(path string, max int) (string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("readdir %s: %w", path, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	// dirs first, then alpha — so when truncated the navigation targets survive.
	sort.Slice(names, func(i, j int) bool {
		di := strings.HasSuffix(names[i], "/")
		dj := strings.HasSuffix(names[j], "/")
		if di != dj {
			return di
		}
		return names[i] < names[j]
	})
	truncated := 0
	if max > 0 && len(names) > max {
		truncated = len(names) - max
		names = names[:max]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n", filepath.Clean(path))
	for _, n := range names {
		b.WriteString(n)
		b.WriteByte('\n')
	}
	if truncated > 0 {
		fmt.Fprintf(&b, "(%d more entries not shown; narrow with a subpath or use grep/find)\n", truncated)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// isBinary reports whether a buffer looks like binary data. Standard heuristic:
// a NUL byte or a high ratio of non-text bytes.
func isBinary(buf []byte) bool {
	if len(buf) == 0 {
		return false
	}
	if bytes.IndexByte(buf, 0) >= 0 {
		return true
	}
	nonText := 0
	for _, b := range buf {
		if b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\b' {
			continue
		}
		if b < 0x20 || b == 0x7f {
			nonText++
		}
	}
	return nonText*100/len(buf) > 30
}

func boolArg(input map[string]any, key string, def bool) bool {
	v, ok := input[key]
	if !ok {
		return def
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}
