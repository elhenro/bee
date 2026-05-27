package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestClassifyToolError_NotFound(t *testing.T) {
	_, err := os.Open("/definitely/not/here/xyzzy")
	if err == nil {
		t.Fatal("expected open error")
	}
	if got := classifyToolError(err); got != toolErrNotFound {
		t.Fatalf("classify: want %q, got %q", toolErrNotFound, got)
	}
}

func TestClassifyToolError_PermissionDenied(t *testing.T) {
	err := fmt.Errorf("open /etc/shadow: %w", os.ErrPermission)
	if got := classifyToolError(err); got != toolErrPermissionDenied {
		t.Fatalf("classify: want %q, got %q", toolErrPermissionDenied, got)
	}
}

func TestClassifyToolError_Timeout(t *testing.T) {
	err := fmt.Errorf("ran too long: %w", context.DeadlineExceeded)
	if got := classifyToolError(err); got != toolErrTimeout {
		t.Fatalf("classify: want %q, got %q", toolErrTimeout, got)
	}
}

func TestClassifyToolError_InvalidArg(t *testing.T) {
	err := errors.New("missing required field: path")
	if got := classifyToolError(err); got != toolErrInvalidArg {
		t.Fatalf("classify: want %q, got %q", toolErrInvalidArg, got)
	}
}

func TestClassifyToolError_ParseError(t *testing.T) {
	var jse *json.SyntaxError
	if err := json.Unmarshal([]byte("{"), &struct{}{}); err != nil {
		if !errors.As(err, &jse) {
			t.Fatalf("expected json.SyntaxError, got %T", err)
		}
		if got := classifyToolError(err); got != toolErrParseError {
			t.Fatalf("classify: want %q, got %q", toolErrParseError, got)
		}
	}
}

func TestClassifyToolError_Unknown(t *testing.T) {
	err := errors.New("something exotic")
	if got := classifyToolError(err); got != toolErrUnknown {
		t.Fatalf("classify: want %q, got %q", toolErrUnknown, got)
	}
}

func TestFormatToolError_ContainsTagsAndSuggestion(t *testing.T) {
	err := fmt.Errorf("open foo: %w", os.ErrNotExist)
	got := formatToolError("read", err)
	if !strings.Contains(got, "[tool=read]") {
		t.Fatalf("missing tool tag: %q", got)
	}
	if !strings.Contains(got, "[type="+toolErrNotFound+"]") {
		t.Fatalf("missing type tag: %q", got)
	}
	if !strings.Contains(got, "suggestion: check the path") {
		t.Fatalf("missing suggestion: %q", got)
	}
}

// regression: workspace-escape and write-filter denials must classify as
// invalid_arg with a workspace-aware suggestion so the model can self-correct.
// previously fell through to toolErrUnknown with no hint → two-strike bail.
func TestClassifyToolError_WorkspaceEscape(t *testing.T) {
	err := errors.New(`path "/tmp/foo" escapes workspace root "/repo"`)
	if got := classifyToolError(err); got != toolErrInvalidArg {
		t.Fatalf("classify: want %q, got %q", toolErrInvalidArg, got)
	}
}

func TestClassifyToolError_WriteFilterDenied(t *testing.T) {
	err := errors.New(`path "foo.exe" denied by write filter`)
	if got := classifyToolError(err); got != toolErrInvalidArg {
		t.Fatalf("classify: want %q, got %q", toolErrInvalidArg, got)
	}
}

func TestFormatToolError_WorkspaceEscapeHasWorkspaceHint(t *testing.T) {
	err := errors.New(`path "/tmp/x" escapes workspace root "/repo"`)
	got := formatToolError("write", err)
	if !strings.Contains(got, "[type="+toolErrInvalidArg+"]") {
		t.Errorf("want invalid_arg class; got: %q", got)
	}
	if !strings.Contains(got, "workspace") {
		t.Errorf("suggestion must mention workspace; got: %q", got)
	}
}

func TestFormatToolError_NoSuggestionForUnknown(t *testing.T) {
	err := errors.New("opaque failure")
	got := formatToolError("bash", err)
	if strings.Contains(got, "suggestion:") {
		t.Fatalf("unknown class should omit suggestion, got: %q", got)
	}
}
