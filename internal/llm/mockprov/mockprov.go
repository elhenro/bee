// Package mockprov is a scripted, deterministic llm.Provider for tests.
//
// Drives multi-turn agent flows from a JSON fixture without touching the
// network. Each call to Stream pops the next scenario whose matcher accepts
// the request, then emits its scripted events in order. Used by integration
// and end-to-end tests to make every tool-dispatch path reproducible.
//
// Fixture shape (mock_scenarios/*.json):
//
//	{
//	  "scenarios": [
//	    {
//	      "name": "greet",
//	      "match": {"contains": "hello"},
//	      "events": [
//	        {"type": "text_delta", "delta": "hi"},
//	        {"type": "done", "stop_reason": "stop",
//	         "usage": {"input": 5, "output": 1}}
//	      ]
//	    }
//	  ]
//	}
//
// Matcher dimensions (all optional, all must hold when present):
//   - contains : substring of the last text block in the trailing message
//   - role     : "user" | "tool" — role of the trailing message
//   - any      : true → accept unconditionally
//
// Consumption is linear: each Stream call advances an internal cursor to the
// first un-consumed scenario whose matcher accepts the request. Mismatch is
// a hard error — the whole point is fail-fast regression detection.
package mockprov

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// File is the on-disk fixture format.
type File struct {
	Scenarios []Scenario `json:"scenarios"`
}

// Scenario is one model turn: a matcher + the events to emit.
type Scenario struct {
	Name   string   `json:"name"`
	Match  Matcher  `json:"match"`
	Events []Event  `json:"events"`
}

// Matcher constrains which Stream request this scenario accepts. Zero value
// matches nothing (use Any:true to accept anything).
type Matcher struct {
	Any      bool   `json:"any,omitempty"`
	Contains string `json:"contains,omitempty"`
	Role     string `json:"role,omitempty"`
}

// Event is one element of the model's scripted output stream.
type Event struct {
	Type       string    `json:"type"`
	Delta      string    `json:"delta,omitempty"`
	Tool       *ToolCall `json:"tool,omitempty"`
	StopReason string    `json:"stop_reason,omitempty"`
	Usage      *Usage    `json:"usage,omitempty"`
}

// ToolCall mirrors types.ToolUse but with a JSON-friendly Input field.
type ToolCall struct {
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// Usage is the token count emitted alongside a done event.
type Usage struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

// Load reads and parses a fixture file.
func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("mockprov: read %s: %w", path, err)
	}
	return Parse(b)
}

// Parse decodes fixture bytes.
func Parse(b []byte) (*File, error) {
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("mockprov: parse: %w", err)
	}
	if len(f.Scenarios) == 0 {
		return nil, fmt.Errorf("mockprov: fixture has no scenarios")
	}
	return &f, nil
}

// Accepts reports whether the matcher accepts the request.
func (m Matcher) Accepts(req llm.Request) bool {
	if m.Any {
		return true
	}
	last, lastText, lastRole := trailing(req.Messages)
	if !last {
		return false
	}
	if m.Role != "" && string(lastRole) != m.Role {
		return false
	}
	if m.Contains != "" && !strings.Contains(lastText, m.Contains) {
		return false
	}
	// at least one constraint must be set, or it never matches
	return m.Role != "" || m.Contains != ""
}

// trailing returns the last message's joined text and role.
func trailing(msgs []types.Message) (ok bool, text string, role types.Role) {
	if len(msgs) == 0 {
		return false, "", ""
	}
	m := msgs[len(msgs)-1]
	var b strings.Builder
	for _, c := range m.Content {
		switch c.Type {
		case types.BlockText:
			b.WriteString(c.Text)
		case types.BlockToolResult:
			if c.Result != nil {
				b.WriteString(c.Result.Content)
			}
		}
	}
	return true, b.String(), m.Role
}
