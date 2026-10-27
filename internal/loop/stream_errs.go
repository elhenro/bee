package loop

import "strings"

// isTransientStreamErr returns true for momentary network / provider hiccups.
// Safe to retry only before any content was emitted — caller's responsibility.
func isTransientStreamErr(err error) bool {
	return err != nil && containsAny(err.Error(),
		"sse scan",
		"stream stalled",
		"context deadline",
		"Client.Timeout",
		"EOF",
		"connection reset",
		"broken pipe",
		"i/o timeout",
		"use of closed network",
	)
}

// isProviderRejectErr reports whether err is a wire-level request-body
// rejection (HTTP 400 from the upstream with markers that the payload was
// malformed) rather than a transport / auth / rate-limit failure. We rely on
// the body text the providers include in 4xx responses (different per vendor
// but all share a small set of phrases), and on a status 400 specifically —
// 401/403/429/5xx have different recovery paths.
//
// Why this is a separate predicate: replaying the same transcript 400s
// forever. The first turn that produced a tool_use with invalid arguments
// was persisted to the session; on resume we must strip that tool_use block
// (or the model must re-emit it) before the next provider call has a chance
// of succeeding. ErrProviderReject is the signal the resume path uses to
// trigger that cleanup.
func isProviderRejectErr(err error) bool {
	if err == nil || isContextOverflowErr(err) {
		return false
	}
	s := err.Error()
	// openai_compat and the chatgpt/claude adapters all format the wire
	// error as "provider <name> status 400: <body>". look for the 400 +
	// a body shape that names the bad field. be conservative: prefer
	// phrases that explicitly finger tool arguments, not generic 400s
	// (auth/quota/context size/etc), and not the bare error envelope names,
	// which wrap those unrelated 400s too. when in doubt, fall through and
	// treat as non-recoverable so the user sees the raw error.
	return strings.Contains(s, "status 400") && containsAny(s,
		"invalid function arguments", // openai-compat openrouter / minimax
		"invalid tool_call",          // openai responses API
		"invalid tool call",          // anthropic
		"tool_call_id",               // minimax: "<phrase>, tool_call_id: <id>"
		"tool_use.input",             // anthropic: "messages.N.content.M.tool_use.input: ..."
		"tools.0.function.arguments", // openai strict mode
	)
}

// isContextOverflowErr reports whether err says the request exceeded the
// model's context window (or the request size limit). Distinct from a
// malformed-payload reject: stripping the last tool call cannot help, the
// history has to shrink.
func isContextOverflowErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	if !strings.Contains(s, "status 400") && !strings.Contains(s, "status 413") {
		return false
	}
	return containsAny(s,
		"context_length_exceeded",
		"prompt is too long",
		"maximum context length",
		"exceeds the context window",
		"too many tokens",
		"request_too_large",
		"exceed context limit",
		"exceeds the maximum number of tokens",
	)
}
