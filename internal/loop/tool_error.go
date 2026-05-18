package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// tool error type tags surfaced to the model. kept short so prompts can
// pattern-match without parsing prose.
const (
	toolErrNotFound         = "not_found"
	toolErrPermissionDenied = "permission_denied"
	toolErrTimeout          = "timeout"
	toolErrInvalidArg       = "invalid_arg"
	toolErrParseError       = "parse_error"
	toolErrUnknown          = "unknown"
)

// classifyToolError maps a raw tool error to one of the toolErr* tags.
// Order matters: more specific checks come first so a wrapped json error
// nested under a not-found doesn't get mis-tagged.
func classifyToolError(err error) string {
	if err == nil {
		return toolErrUnknown
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return toolErrTimeout
	case errors.Is(err, os.ErrNotExist) || os.IsNotExist(err):
		return toolErrNotFound
	case errors.Is(err, os.ErrPermission) || os.IsPermission(err):
		return toolErrPermissionDenied
	}
	var je *json.SyntaxError
	if errors.As(err, &je) {
		return toolErrParseError
	}
	var jt *json.UnmarshalTypeError
	if errors.As(err, &jt) {
		return toolErrParseError
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "invalid argument"),
		strings.Contains(msg, "missing required"),
		strings.Contains(msg, "missing field"),
		strings.Contains(msg, "argparse"),
		strings.Contains(msg, "unexpected type"):
		return toolErrInvalidArg
	case strings.Contains(msg, "json"):
		return toolErrParseError
	case strings.Contains(msg, "no such file"),
		strings.Contains(msg, "not found"):
		return toolErrNotFound
	case strings.Contains(msg, "permission denied"):
		return toolErrPermissionDenied
	case strings.Contains(msg, "timeout"),
		strings.Contains(msg, "deadline exceeded"):
		return toolErrTimeout
	}
	return toolErrUnknown
}

// suggestionFor returns a one-liner recovery hint keyed to the error class.
// Empty string means no hint to surface.
func suggestionFor(class string) string {
	switch class {
	case toolErrNotFound:
		return "check the path"
	case toolErrInvalidArg:
		return "inspect tool schema"
	case toolErrParseError:
		return "ensure valid json"
	case toolErrPermissionDenied:
		return "verify read/write access"
	case toolErrTimeout:
		return "narrow scope or retry"
	}
	return ""
}

// formatToolError renders the compact text envelope the model sees on a
// tool failure. Single line, parseable by simple substring checks.
func formatToolError(toolName string, err error) string {
	class := classifyToolError(err)
	msg := err.Error()
	hint := suggestionFor(class)
	out := fmt.Sprintf("[tool=%s] [type=%s] %s", toolName, class, msg)
	if hint != "" {
		out += ". suggestion: " + hint
	}
	return out
}
