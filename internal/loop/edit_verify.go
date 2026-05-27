package loop

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// editVerifyThreshold is the number of consecutive edits to the same file
// without an intervening verify (build/test/read of that path) before a
// nudge fires. 3 matches the pattern observed in session 5e20f3f8 where
// the agent edited web_fetch.go 5+ times in a row chasing a formatting
// edge case without re-running tests between.
const editVerifyThreshold = 3

// editingTools are tool names that mutate a file targeted by an input
// "path" arg. Subset of mutatorTools — `bash` and `apply_patch` also
// mutate but don't have a stable path field we can key on.
var editingTools = map[string]bool{
	"edit":          true,
	"hashline_edit": true,
	"write":         true,
}

// verifyBashRe matches shell commands that exercise the code: build, test,
// vet, lint. When one of these runs, all edit counts reset — the model has
// observed real behavior, so the loop ends.
//
// Deliberately broad: matches `go test`, `go build`, `go vet`, `npm test`,
// `pnpm test`, `yarn test`, `pytest`, `cargo test`, `cargo build`, `make`.
// Misses are tolerable — at worst the nudge fires once too often.
var verifyBashRe = regexp.MustCompile(`(?i)\b(go\s+(test|build|vet|run)|npm\s+(test|run\s+build)|pnpm\s+(test|build)|yarn\s+(test|build)|pytest|cargo\s+(test|build|check)|make\b)`)

// observeEditNoVerify increments per-file edit counts for editing tools and
// resets them when a verify happens. Returns blocks unchanged when nothing
// crosses the threshold, otherwise prepends a one-shot nudge per file.
//
// Path normalization: stripped of trailing slashes, but otherwise keyed
// verbatim so `./foo.go` and `/abs/path/foo.go` count separately. Model
// usually picks one form per session, so cross-form collisions are rare —
// and a missed match just means the nudge fires later.
func observeEditNoVerify(e *Engine, uses []types.ToolUse, results []types.ToolResult, blocks []types.ContentBlock) []types.ContentBlock {
	if e.editsByFile == nil {
		e.editsByFile = make(map[string]int)
	}
	if e.nudgedEditNoVerify == nil {
		e.nudgedEditNoVerify = make(map[string]bool)
	}
	// build a quick lookup of UseID → result so we can skip failed edits
	// (a failed write doesn't change the file on disk and shouldn't count).
	resByID := make(map[string]bool, len(results))
	for _, r := range results {
		resByID[r.UseID] = r.IsError
	}
	// pass 1: any verify-like activity resets the affected file's counter.
	// reads reset only their own path; bash verifies reset everything (a
	// build/test is project-wide signal).
	for _, u := range uses {
		if u.Name == "read" {
			if p, ok := u.Input["path"].(string); ok && p != "" {
				delete(e.editsByFile, normalizeEditPath(p))
				delete(e.nudgedEditNoVerify, normalizeEditPath(p))
			}
			continue
		}
		if u.Name == "bash" {
			cmd, _ := u.Input["command"].(string)
			if verifyBashRe.MatchString(cmd) {
				e.editsByFile = make(map[string]int)
				e.nudgedEditNoVerify = make(map[string]bool)
			}
		}
	}
	// pass 2: increment for successful edits and emit nudges on threshold.
	for _, u := range uses {
		if !editingTools[u.Name] {
			continue
		}
		if resByID[u.ID] {
			continue // edit failed, don't count
		}
		p, _ := u.Input["path"].(string)
		if p == "" {
			continue
		}
		key := normalizeEditPath(p)
		e.editsByFile[key]++
		if e.editsByFile[key] >= editVerifyThreshold && !e.nudgedEditNoVerify[key] {
			w := fmt.Sprintf("[verify] %d edits to %s without running build/test. run it before more edits — small fixes can cascade.\n\n",
				e.editsByFile[key], p)
			blocks = prependWarningToToolResult(blocks, w)
			e.nudgedEditNoVerify[key] = true
		}
	}
	return blocks
}

func normalizeEditPath(p string) string {
	return strings.TrimRight(strings.TrimSpace(p), "/")
}
