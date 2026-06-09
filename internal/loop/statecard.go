package loop

import (
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/types"
)

// stateCard keeps the request context O(1) for small models: instead of the
// rolling transcript, each request carries [card, last few raw messages].
// The card is rebuilt deterministically from structured per-iteration data
// (tool calls + results) — no side-LLM summarize, no compaction cliff, and a
// 4k-window model can run hundreds of iterations. The on-disk session and the
// TUI scrollback keep the full history; the card only shapes what goes on the
// wire. Older detail is recoverable by the model re-reading files.
type stateCard struct {
	goal      string
	actions   []string          // newest last, capped at stateCardMaxActions
	files     map[string]string // path -> strongest verb seen (read|edited|written)
	fileOrder []string
	lastError string
	calls     int
	turns     int
}

const (
	// stateCardKeepMsgs raw tail messages stay verbatim in the view, so the
	// model always sees its own last calls + results exactly.
	stateCardKeepMsgs    = 6
	stateCardMaxActions  = 8
	stateCardMaxFiles    = 10
	stateCardArgChars    = 60
	stateCardResultChars = 100
	stateCardErrChars    = 200
)

func newStateCard(goal string) *stateCard {
	return &stateCard{goal: goal, files: map[string]string{}}
}

// compactionEnabled gates auto-compact. The state card bounds the request
// itself, so the side-LLM summarize round-trip would burn slow-local-model
// minutes shrinking a transcript that never reaches the wire anyway.
func (e *Engine) compactionEnabled() bool {
	return e.Cfg.Compaction.Enabled && !config.ActiveProfile(e.Cfg).StateCard
}

// observe folds one iteration's tool batch into the card. uses and results
// align by index (dispatchTools keeps order).
func (c *stateCard) observe(uses []types.ToolUse, results []types.ToolResult) {
	c.turns++
	for i, u := range uses {
		c.calls++
		line := u.Name + "(" + repArg(u.Input) + ")"
		if i < len(results) {
			r := results[i]
			if r.IsError {
				line += " ERR: " + firstLine(r.Content, stateCardResultChars)
				c.lastError = u.Name + ": " + firstLine(r.Content, stateCardErrChars)
			} else {
				line += " ok"
			}
		}
		c.actions = append(c.actions, line)
		c.noteFile(u)
	}
	if over := len(c.actions) - stateCardMaxActions; over > 0 {
		c.actions = append([]string(nil), c.actions[over:]...)
	}
}

// fileVerbRank orders verbs so a later weaker touch never downgrades the card
// entry (written > edited > read).
var fileVerbRank = map[string]int{"read": 1, "edited": 2, "written": 3}

func (c *stateCard) noteFile(u types.ToolUse) {
	var verb string
	switch u.Name {
	case "read":
		verb = "read"
	case "edit", "edit_diff", "hashline_edit", "apply_patch":
		verb = "edited"
	case "write":
		verb = "written"
	default:
		return
	}
	path, _ := u.Input["path"].(string)
	if path == "" {
		path, _ = u.Input["file_path"].(string)
	}
	if path == "" {
		return
	}
	if prev, seen := c.files[path]; !seen {
		if len(c.files) >= stateCardMaxFiles {
			return
		}
		c.files[path] = verb
		c.fileOrder = append(c.fileOrder, path)
	} else if fileVerbRank[verb] > fileVerbRank[prev] {
		c.files[path] = verb
	}
}

// render emits the card text. lowercase, terse — it rides every request on a
// tiny window, so every byte counts.
func (c *stateCard) render() string {
	var b strings.Builder
	b.WriteString("[state card] older turns folded into this summary; re-read files for detail.\n")
	b.WriteString("goal: ")
	b.WriteString(c.goal)
	b.WriteString("\n")
	if len(c.fileOrder) > 0 {
		b.WriteString("files: ")
		for i, p := range c.fileOrder {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(p + "(" + c.files[p] + ")")
		}
		b.WriteString("\n")
	}
	if len(c.actions) > 0 {
		b.WriteString("recent actions:\n")
		for _, a := range c.actions {
			b.WriteString("- " + a + "\n")
		}
	}
	if c.lastError != "" {
		b.WriteString("last error: " + c.lastError + "\n")
	}
	fmt.Fprintf(&b, "progress: %d tool calls over %d turns\n", c.calls, c.turns)
	return b.String()
}

// view builds the wire-bound message slice: [card, last stateCardKeepMsgs raw
// messages]. The tail start walks back past leading RoleTool messages so a
// tool_result is never sent without the assistant tool_use it answers — wire
// formats reject orphan results.
func (c *stateCard) view(msgs []types.Message) []types.Message {
	start := len(msgs) - stateCardKeepMsgs
	if start < 0 {
		start = 0
	}
	for start > 0 && msgs[start].Role == types.RoleTool {
		start--
	}
	card := types.Message{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: c.render()}},
	}
	out := make([]types.Message, 0, len(msgs)-start+1)
	out = append(out, card)
	out = append(out, msgs[start:]...)
	return out
}

// repArg picks the most informative single argument for the action line.
func repArg(input map[string]any) string {
	for _, k := range []string{"command", "path", "file_path", "pattern", "query", "url", "name"} {
		if v, ok := input[k].(string); ok && v != "" {
			return truncateCard(v, stateCardArgChars)
		}
	}
	return ""
}

func firstLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return truncateCard(s, max)
}

// truncateCard clips to max runes with a bare ellipsis — terser than the
// shared truncate()'s "…[truncated]" marker, which would bloat every card line.
func truncateCard(s string, max int) string {
	count := 0
	for i := range s {
		if count == max {
			return s[:i] + "…"
		}
		count++
	}
	return s
}
