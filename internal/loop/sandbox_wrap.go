package loop

import (
	"strings"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/sandbox"
)

// wrapShellInput injects the sandbox-wrapped command into the shell tool
// input. degrades silently when wrapping is unsupported — sandbox is
// hardening, not a security gate (see sandbox/policy.go).
func wrapShellInput(input map[string]any, cfg config.SandboxConfig, cwd string) map[string]any {
	cmdStr, ok := input["command"].(string)
	if !ok || cmdStr == "" {
		return input
	}
	scope := sandbox.Scope(cfg.Scope)
	if scope == "" || scope == sandbox.DangerFullAccess {
		return input
	}
	pol := sandbox.Policy{
		Scope:    scope,
		Approval: sandbox.ApprovalMode(cfg.Approval),
		Cwd:      cwd,
	}
	wrapped, err := sandbox.Wrap(pol, []string{"bash", "-c", cmdStr})
	if err != nil {
		return input
	}
	if len(wrapped) <= 3 {
		// nothing actually wrapped; preserve original input shape
		return input
	}
	// rebuild a single bash -c string that execs the wrapped argv. quoting
	// is best-effort; sandbox helper paths are well-known and lack metas.
	newCmd := joinShell(wrapped)
	out := make(map[string]any, len(input)+2)
	for k, v := range input {
		out[k] = v
	}
	out["command"] = newCmd
	// keep the unwrapped command for the approval modal + danger detect, so
	// the user sees `rm -rf foo`, not the sandbox-exec profile blob.
	out["_orig_command"] = cmdStr
	return out
}

func joinShell(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = shellQuote(a)
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '/' || r == '-' || r == '_' || r == '.' || r == ':' || r == '=' {
			continue
		}
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return s
}
