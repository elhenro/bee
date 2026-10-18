package types

import "strings"

// CallLabelKey is the optional presentation arg a model fills on a call to
// say in a few words what it is for ("running the snake tests"). Display
// only: it is stripped before a tool runs and ignored when comparing calls.
const CallLabelKey = "description"

// calls whose card is clearer with a label than with their raw args
var labeledTools = map[string]bool{
	"bash":       true,
	"search":     true,
	"glob":       true,
	"web_fetch":  true,
	"web_search": true,
	"codegraph":  true,
}

// LabelsCalls reports whether the model is offered a label for tool.
func LabelsCalls(tool string) bool { return labeledTools[tool] }

// Label is the call's label as one trimmed line; empty when the tool takes
// none or the model left it out.
func (u ToolUse) Label() string {
	if !labeledTools[u.Name] {
		return ""
	}
	s, _ := u.Input[CallLabelKey].(string)
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(s)
}

// Args is Input without the label, for running, validating and comparing a
// call. The same map when there is nothing to strip.
func (u ToolUse) Args() map[string]any {
	if !labeledTools[u.Name] {
		return u.Input
	}
	if _, ok := u.Input[CallLabelKey]; !ok {
		return u.Input
	}
	out := make(map[string]any, len(u.Input))
	for k, v := range u.Input {
		if k != CallLabelKey {
			out[k] = v
		}
	}
	return out
}
