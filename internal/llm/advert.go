package llm

import (
	"slices"
	"strings"
)

// mergeSystem appends the injected block to an existing system prompt with a
// blank line between. Empty system → injection only.
func mergeSystem(sys, inj string) string {
	sys = strings.TrimRight(sys, "\n")
	if sys == "" {
		return inj
	}
	return sys + "\n\n" + inj
}

// buildToolInstruction renders the text-mode tool advertisement. Order is
// preserved from req.Tools so callers can prioritize. Parameter names + types
// from each tool's JSON Schema are surfaced so the model emits the right keys
// instead of guessing (qwen3 used to invent `{"args":{"cmd":"..."}}` here).
func buildToolInstruction(tools []ToolSpec, extra string) string {
	var b strings.Builder
	b.WriteString("## Tools (text format)\n")
	b.WriteString("Call a tool by emitting EXACTLY one XML block per call. The tag NAME is the tool's name verbatim (e.g. <bash>, <edit>, <write>) — NOT the literal string \"tool_name\":\n")
	b.WriteString("<TOOLNAME>{\"arg\":\"value\"}</TOOLNAME>\n\n")
	b.WriteString("Args must use EXACT parameter names from each tool's signature below.\n")
	b.WriteString("DO NOT invent keys like `args`/`cmd`/`input` — use the schema names verbatim.\n")
	b.WriteString("Accepted alternative shapes (parsed as fallback):\n")
	b.WriteString("- bare JSON: `{\"name\":\"tool\",\"arguments\":{...}}`\n")
	b.WriteString("- hermes wrapper: `<tool_call>{\"name\":\"tool\",\"arguments\":{...}}</tool_call>`\n")
	b.WriteString("- hermes xml: `<tool_call><function=NAME><parameter=KEY>VALUE</parameter></function></tool_call>`\n")
	b.WriteString("  (qwen3 / hermes chat-template native — use if your training prefers it)\n\n")
	b.WriteString("Do NOT wrap commands in markdown fences (```bash ... ```) — fences are NOT parsed as tool calls.\n\n")
	b.WriteString("Available tools:\n")
	b.WriteString(toolList(tools))
	b.WriteString("\nEmit ONLY the XML, nothing else when calling a tool. No prose before or after the tag.\n")
	// 1-shot exemplar: 3B-active MoEs (qwen3-a3b, etc.) lock onto literal
	// shapes more reliably than abstract spec. Anchor on the most common
	// tool name (bash) so the template imprints — saves a re-try on first call.
	b.WriteString("\nExample (verbatim shape — match exactly):\n")
	b.WriteString("<bash>{\"command\":\"ls -la\"}</bash>\n")
	if extra != "" {
		b.WriteString("\n")
		b.WriteString(extra)
		b.WriteString("\n")
	}
	return b.String()
}

// toolList renders one `- name(params): description` line per tool for the
// prompt advert.
func toolList(tools []ToolSpec) string {
	var b strings.Builder
	for _, t := range tools {
		desc := t.PromptSnippet
		if desc == "" {
			desc = firstSentence(t.Description)
		}
		b.WriteString("- " + t.Name + renderSchemaSig(t.Schema) + ": " + desc + "\n")
	}
	return b.String()
}

// renderSchemaSig turns a JSON Schema object into a compact param signature.
// e.g. `(command:string, [timeout_seconds:integer], [cwd:string])`.
// Required params first, optional in `[brackets]`, each group sorted so
// transcripts diff cleanly. Returns "" for empty/no properties so tools
// without schema fall back to bare `- name: desc`.
func renderSchemaSig(schema map[string]any) string {
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return ""
	}
	required := map[string]bool{}
	switch rs := schema["required"].(type) {
	case []any:
		for _, r := range rs {
			if s, ok := r.(string); ok {
				required[s] = true
			}
		}
	case []string:
		for _, s := range rs {
			required[s] = true
		}
	}
	var req, opt []string
	for name, raw := range props {
		typ := "any"
		m, _ := raw.(map[string]any)
		if t, _ := m["type"].(string); t != "" {
			typ = t
		}
		entry := name + ":" + typ
		if required[name] {
			req = append(req, entry)
		} else {
			opt = append(opt, "["+entry+"]")
		}
	}
	slices.Sort(req)
	slices.Sort(opt)
	return "(" + strings.Join(append(req, opt...), ", ") + ")"
}

// firstSentence picks a short lead from a tool's description so the advert
// stays narrow. Prefers the first newline; otherwise cuts at a period that
// is followed by whitespace so URLs (`https://example.com/foo`) and version
// strings (`v1.2.3`) don't get sliced mid-token. Returns the full string
// when neither marker is found.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	for i := 0; i < len(s)-1; i++ {
		if s[i] != '.' {
			continue
		}
		switch s[i+1] {
		case ' ', '\t':
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}
