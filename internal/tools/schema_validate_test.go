package tools

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/llm"
)

func shellSpec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: "bash",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string"},
				"timeout_seconds": map[string]any{"type": "integer"},
				"cwd": map[string]any{"type": "string"},
			},
			"required": []string{"command"},
		},
	}
}

func TestValidateInput_OK(t *testing.T) {
	spec := shellSpec()
	if err := ValidateInput(spec, map[string]any{"command": "ls"}); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if err := ValidateInput(spec, map[string]any{"command": "ls", "timeout_seconds": float64(10)}); err != nil {
		t.Fatalf("expected ok with optional, got %v", err)
	}
}

func TestValidateInput_MissingRequired(t *testing.T) {
	spec := shellSpec()
	err := ValidateInput(spec, map[string]any{"cmd": "ls"})
	if err == nil {
		t.Fatal("expected error on missing required")
	}
	msg := err.Error()
	if !strings.Contains(msg, `missing required "command"`) {
		t.Errorf("expected missing-required problem, got %q", msg)
	}
	if !strings.Contains(msg, `<bash>{"command":"ls -la"}</bash>`) {
		t.Errorf("expected example envelope with corrected key, got %q", msg)
	}
}

func TestValidateInput_EmptyRequired(t *testing.T) {
	spec := shellSpec()
	err := ValidateInput(spec, map[string]any{"command": "   "})
	if err == nil {
		t.Fatal("expected error on empty required")
	}
	if !strings.Contains(err.Error(), `empty "command"`) {
		t.Errorf("expected empty problem, got %q", err.Error())
	}
}

func TestValidateInput_WrongType(t *testing.T) {
	spec := shellSpec()
	err := ValidateInput(spec, map[string]any{"command": "ls", "timeout_seconds": "thirty"})
	if err == nil {
		t.Fatal("expected error on wrong type")
	}
	if !strings.Contains(err.Error(), `wrong type for "timeout_seconds"`) {
		t.Errorf("expected wrong-type problem, got %q", err.Error())
	}
}

func TestValidateInput_IntegerAcceptsFloat64(t *testing.T) {
	// json.Unmarshal yields float64 for all numbers — integers must accept it.
	spec := shellSpec()
	if err := ValidateInput(spec, map[string]any{"command": "ls", "timeout_seconds": float64(30)}); err != nil {
		t.Fatalf("expected ok with float64 integer, got %v", err)
	}
}

func TestValidateInput_UnknownKeysTolerated(t *testing.T) {
	spec := shellSpec()
	if err := ValidateInput(spec, map[string]any{"command": "ls", "_parse_error": "x"}); err != nil {
		t.Fatalf("expected ok with _parse_error key, got %v", err)
	}
	if err := ValidateInput(spec, map[string]any{"command": "ls", "extra": "stuff"}); err != nil {
		t.Fatalf("expected ok with unknown key, got %v", err)
	}
}

func TestValidateInput_NoSchema(t *testing.T) {
	if err := ValidateInput(llm.ToolSpec{Name: "x"}, map[string]any{}); err != nil {
		t.Fatalf("expected ok when no schema, got %v", err)
	}
}

func TestRenderExampleEnvelope_PathHeuristic(t *testing.T) {
	spec := llm.ToolSpec{
		Name: "read",
		Schema: map[string]any{
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
			"required": []string{"path"},
		},
	}
	err := ValidateInput(spec, map[string]any{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `"path":"./path/to/file"`) {
		t.Errorf("expected path heuristic in example, got %q", err.Error())
	}
}

func TestRenderExampleEnvelope_MultiRequiredOrdered(t *testing.T) {
	spec := llm.ToolSpec{
		Name: "edit",
		Schema: map[string]any{
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
				"old":  map[string]any{"type": "string"},
				"new":  map[string]any{"type": "string"},
			},
			"required": []string{"path", "old", "new"},
		},
	}
	err := ValidateInput(spec, map[string]any{})
	if err == nil {
		t.Fatal("expected error")
	}
	// example should preserve required order, not alphabetic
	idxPath := strings.Index(err.Error(), `"path"`)
	idxOld := strings.Index(err.Error(), `"old"`)
	idxNew := strings.Index(err.Error(), `"new"`)
	if !(idxPath < idxOld && idxOld < idxNew) {
		t.Errorf("expected required-key order path < old < new in example, got %q", err.Error())
	}
}
