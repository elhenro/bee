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
	if err == nil {
		return false
	}
	s := err.Error()
	// openai_compat and the chatgpt/claude adapters all format the wire
	// error as "provider <name> status 400: <body>". look for the 400 +
	// a body shape that names the bad field. be conservative: prefer
	// phrases that explicitly finger tool arguments, not generic 400s
	// (auth/quota/etc). when in doubt, fall through and treat as
	// non-recoverable so the user sees the raw error.
	return strings.Contains(s, "status 400") && containsAny(s,
		"invalid function arguments", // openai-compat openrouter / minimax
		"invalid tool_call",          // openai responses API
		"invalid tool call",          // anthropic
		"tool_call_id",               // minimax: "<phrase>, tool_call_id: <id>"
		"invalid_request_error",      // anthropic envelope
		"invalid parameters",         // openai envelope
		"tools.0.function.arguments", // openai strict mode
		"invalid_argument",           // generic
	)
}
