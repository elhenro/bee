// Package safety contains defense-in-depth guards for tool calls: secret
// redaction on tool output, and path / shell-command checks that refuse to
// read or mutate obviously sensitive targets even when the sandbox allows it.
//
// Patterns and approach ported from crynta/terax-ai (Apache-2.0).
package safety
