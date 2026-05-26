package loop

import (
	"fmt"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/types"
)

// writeArgTools maps tool names to the input keys that identify what was
// written. The body hash uses a simple "{key1}={value};..." canonicalization
// over these keys — enough to spot exact-content dups without parsing every
// tool's payload shape.
var writeArgTools = map[string][]string{
	"write":         {"path", "content"},
	"edit":          {"path", "old_string", "new_string"},
	"hashline_edit": {"path", "hash", "new_lines"},
	"apply_patch":   {"patch"},
}

// readPathTools maps read-side tool names → input key that holds the path.
// observing one of these clears dup-eligibility for that path: the model may
// have observed a state change and intentionally re-applied content.
var readPathTools = map[string]string{
	"read": "path",
}

// observeDuplicateWrites walks paired toolUses + results, feeds writes/reads
// into the per-Run tracker, and prepends a [dup-write] warning to blocks when
// the model commits identical content to the same path twice with no read in
// between. opt-in via active profile's Safety.WarnOnDuplicateWrites.
func observeDuplicateWrites(e *Engine, uses []types.ToolUse, results []types.ToolResult, blocks []types.ContentBlock) []types.ContentBlock {
	if !config.ActiveProfile(e.Cfg).Safety.WarnOnDuplicateWrites {
		return blocks
	}
	if e.dupWrites == nil {
		e.dupWrites = newDuplicateWriteTracker()
	}
	byUseID := make(map[string]bool, len(results))
	for _, r := range results {
		byUseID[r.UseID] = r.IsError
	}
	for _, u := range uses {
		if k, ok := readPathTools[u.Name]; ok {
			if p, _ := u.Input[k].(string); p != "" {
				e.dupWrites.ObserveRead(p)
			}
			continue
		}
		keys, ok := writeArgTools[u.Name]
		if !ok || byUseID[u.ID] {
			continue
		}
		path, _ := u.Input["path"].(string)
		if path == "" {
			// apply_patch: no single "path" — skip (the patch text itself acts
			// as the body, fingerprinted via the repeat detector instead).
			continue
		}
		body := canonicalArgsBody(u.Input, keys)
		if e.dupWrites.ObserveWrite(path, body) {
			w := fmt.Sprintf("[dup-write] %s wrote identical content to %s twice with no read in between — confirm the file isn't already at the target state.\n\n", u.Name, path)
			blocks = prependWarningToToolResult(blocks, w)
		}
	}
	return blocks
}

// canonicalArgsBody builds the dup-key body from input over the listed keys.
// not a hash itself — duplicateWriteTracker.hashBody handles the hashing.
func canonicalArgsBody(input map[string]any, keys []string) string {
	var out string
	for _, k := range keys {
		if v, ok := input[k]; ok {
			out += k + "="
			switch s := v.(type) {
			case string:
				out += s
			default:
				out += fmt.Sprintf("%v", s)
			}
			out += ";"
		}
	}
	return out
}
