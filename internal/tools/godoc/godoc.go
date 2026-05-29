// Package godoc implements the godoc tool: query the local Go documentation
// for a package or symbol via `go doc -short`. Acts as a stdlib/module API
// oracle so small models don't hallucinate function names. Session
// 5e20f3f8 burned a build cycle on a phantom `transform.TrimSpace` —
// querying `godoc golang.org/x/text/transform` first would have shown the
// real surface (`Append`, `Bytes`, `String`, ...).
package godoc

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

const (
	toolName       = "godoc"
	defaultTimeout = 5 * time.Second
	maxOutputBytes = 8 * 1024
	truncMarker    = "\n[…truncated]"
)

// Tool runs `go doc` against the active Go toolchain. cwd is bee's cwd so
// module-local imports resolve against the user's project go.mod.
type Tool struct {
	cwd string
}

// New returns a godoc tool rooted at cwd. cwd must be inside (or above) a
// Go module for module-relative lookups to work; stdlib lookups work
// regardless.
func New(cwd string) tools.Tool {
	return &Tool{cwd: cwd}
}

// Spec advertises the tool. PromptSnippet is the prompt-manifest one-liner
// shown to tiny profiles; Description is the full API-spec text shown to
// hosted models that consume native tool schemas.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: toolName,
		Description: "Query Go documentation for a package or symbol via `go doc -short`. " +
			"Use BEFORE writing import statements or calling unfamiliar APIs — prevents phantom-function hallucinations. " +
			"Examples: `strings.TrimSpace`, `fmt`, `golang.org/x/text/transform`, `net/http.Client`. " +
			"Returns the package summary plus function/type signatures. Local module imports resolve against the project's go.mod.",
		PromptSnippet: "Query Go API docs (verify before call)",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"target": map[string]any{
					"type":        "string",
					"minLength":   1,
					"description": "Package import path or symbol. E.g. `fmt`, `strings.TrimSpace`, `golang.org/x/net/html`.",
				},
				"long": map[string]any{
					"type":        "boolean",
					"description": "Include full doc text (omit `-short`). Default false — short is preferred for context economy.",
				},
			},
			"required": []string{"target"},
		},
	}
}

// Run shells out to `go doc`. Combined stdout+stderr returned. Truncated at
// maxOutputBytes so a `go doc <huge-pkg>` can't blow tiny-profile context.
//
// Hard timeout: 5s. `go doc` is local and fast; a slow run usually means a
// fetch attempt for a missing module, which we'd rather fail than hang on.
func (t *Tool) Run(ctx context.Context, input map[string]any) (tools.Result, error) {
	target, ok := input["target"].(string)
	if !ok || strings.TrimSpace(target) == "" {
		return tools.Result{Content: "missing or empty 'target' field", IsError: true}, nil
	}
	target = strings.TrimSpace(target)
	if err := validateTarget(target); err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}

	long, _ := input["long"].(bool)

	cctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	args := []string{"doc"}
	if !long {
		args = append(args, "-short")
	}
	args = append(args, target)
	cmd := exec.CommandContext(cctx, "go", args...)
	cmd.Dir = t.cwd
	// disable Go's automatic module fetch on miss: keeps the oracle hermetic.
	// On a missing module, fail clean with the real error instead of stalling
	// while Go contacts proxy.golang.org. Especially important for tiny
	// profile runs where the user may be offline.
	cmd.Env = append(cmd.Environ(), "GOFLAGS=-mod=readonly", "GOPROXY=off")

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()
	if cctx.Err() == context.DeadlineExceeded {
		return tools.Result{Content: fmt.Sprintf("godoc: timeout after %s", defaultTimeout), IsError: true}, nil
	}
	if err != nil {
		// `go doc` returns non-zero for unknown package/symbol. surface the
		// stderr so the model sees `package X is not in std (...)` and
		// learns the import path isn't real, instead of plowing ahead.
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = err.Error()
		}
		return tools.Result{Content: "godoc: " + msg, IsError: true}, nil
	}

	out = truncate(out, maxOutputBytes)
	return tools.Result{Content: out, IsError: false}, nil
}

// validateTarget rejects shell metacharacters that would let a target string
// smuggle args past `go doc`. exec.Command already passes argv directly (no
// shell), so injection isn't possible — but a target containing `;` or `|`
// is almost certainly a model error, not a real symbol. Surface a clean
// diagnostic instead of letting `go doc` choke on it.
func validateTarget(s string) error {
	if strings.ContainsAny(s, ";|&`$<>\n\r\"'") {
		return fmt.Errorf("godoc: invalid characters in target %q — pass a single package or symbol (e.g. `fmt.Println`)", s)
	}
	if strings.HasPrefix(s, "-") {
		return fmt.Errorf("godoc: target cannot start with `-` (looks like a flag): %q", s)
	}
	return nil
}

// truncate caps output at maxBytes, cutting at the last newline before the
// boundary so we don't slice mid-signature.
func truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := strings.LastIndexByte(s[:maxBytes], '\n')
	if cut <= 0 {
		cut = maxBytes
	}
	return s[:cut] + truncMarker
}
