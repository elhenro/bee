// Package knowledge_write implements the knowledge_write tool: store a record
// in bee's on-disk knowledge store. Records persist across sessions and are
// injected into the system prompt on future turns matching their tags/tokens.
package knowledge_write

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

const toolName = "knowledge_write"

// Tool stores records in the bee knowledge store.
type Tool struct {
	dir string
}

// New constructs the tool. dir is the store directory (from knowledge.StoreDir).
func New(dir string) *Tool {
	return &Tool{dir: dir}
}

// Spec advertises the tool to the model.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        toolName,
		Description: "Store a note in bee's knowledge store. Records persist across sessions and are auto-injected into the system prompt on future turns. Use to save preferences, decisions, conventions, or context you want the agent to remember. Max 5 tags, each lowercase-hyphenated (a-z, 0-9, hyphens).",
		PromptSnippet: "Save a record to bee's knowledge store",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Record name — slug: a-z, A-Z, 0-9, hyphens, underscores, dots. Becomes the filename.",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "One-line summary of what this record covers.",
				},
				"body": map[string]any{
					"type":        "string",
					"description": "Full record content — the information the agent should recall.",
				},
				"tags": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
					},
					"description": "Tags for matching (max 5, lowercase-alphanumeric-hyphenated).",
				},
				"priority": map[string]any{
					"type":        "integer",
					"description": "Importance 1-5 (default 3). Higher = more likely injected.",
				},
				"expires_at": map[string]any{
					"type":        "string",
					"description": "Optional expiry: YYYY-MM-DD or RFC 3339. Expired records flagged in prompt.",
				},
			},
			"required": []string{"name", "description", "body"},
		},
	}
}

// Run writes a record to the knowledge store.
func (t *Tool) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	if t.dir == "" {
		return tools.Result{Content: "knowledge store not configured", IsError: true}, nil
	}

	name, _ := in["name"].(string)
	desc, _ := in["description"].(string)
	body, _ := in["body"].(string)

	if strings.TrimSpace(name) == "" {
		return tools.Result{Content: "missing or empty 'name'", IsError: true}, nil
	}
	if strings.TrimSpace(desc) == "" {
		return tools.Result{Content: "missing or empty 'description'", IsError: true}, nil
	}
	if strings.TrimSpace(body) == "" {
		return tools.Result{Content: "body must be non-empty", IsError: true}, nil
	}

	tags := parseTags(in["tags"])
	priority := parsePriority(in["priority"])
	expiresAt := parseExpires(in["expires_at"])

	rec := knowledge.Record{
		Entry: knowledge.Entry{
			Name:        name,
			Description: desc,
			Tags:        tags,
			Priority:    priority,
			ExpiresAt:   expiresAt,
		},
		Body: body,
	}

	path, err := knowledge.WriteRecord(t.dir, rec)
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("write failed: %v", err), IsError: true}, nil
	}
	return tools.Result{Content: fmt.Sprintf("stored %s at %s", name, path)}, nil
}

// parseTags converts []any to []string, dropping empty entries.
func parseTags(raw any) []string {
	if raw == nil {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			continue
		}
		out = append(out, strings.TrimSpace(s))
	}
	return out
}

// parsePriority converts float64 (JSON number) to int, defaulting to 0 (sentinel).
func parsePriority(raw any) int {
	if raw == nil {
		return 0
	}
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

// parseExpires converts the expires_at input to time.Time. Accepts:
//   - nil / "" → zero time (no expiry)
//   - "YYYY-MM-DD" → midnight UTC
//   - RFC 3339 string
// On parse failure returns zero time (caller may want to reject, but the tool
// spec says "optional" so silent fallthrough matches "no expiry" semantics).
func parseExpires(raw any) time.Time {
	if raw == nil {
		return time.Time{}
	}
	s, ok := raw.(string)
	if !ok || s == "" {
		return time.Time{}
	}
	s = strings.TrimSpace(s)
	if len(s) == 10 && s[4] == '-' && s[7] == '-' {
		t, err := time.Parse("2006-01-02", s)
		if err == nil {
			return t
		}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t
	}
	// unparseable — silently no expiry
	return time.Time{}
}
