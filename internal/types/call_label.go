package types

import (
	"maps"
	"strings"
)

// CallLabelKey is the optional presentation arg a model fills on a call to
// say in a few words what it is for ("running the snake tests"). Display
// only: it is stripped before a tool runs and ignored when comparing calls.
const CallLabelKey = "description"

// WhyKey is the optional one-line reason a model gives on a file mutation.
// Written at the moment of the edit, it rides in the tool-use input so the
// transcript, the session log, the tool card and the review aids all carry
// it. Stripped and ignored exactly like the label; a call without it still
// succeeds.
const WhyKey = "why"

// calls whose card is clearer with a label than with their raw args
var labeledTools = map[string]bool{
	"bash":       true,
	"search":     true,
	"glob":       true,
	"web_fetch":  true,
	"web_search": true,
	"codegraph":  true,
}

// the file-mutating tools, which take a why
var whyTools = map[string]bool{
	"apply_patch":   true,
	"write":         true,
	"edit":          true,
	"hashline_edit": true,
}

// LabelsCalls reports whether the model is offered a label for tool.
func LabelsCalls(tool string) bool { return labeledTools[tool] }

// TakesWhy reports whether the model is offered a why for tool.
func TakesWhy(tool string) bool { return whyTools[tool] }

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

// Why is the call's trimmed reason; empty when the tool takes none or the
// model left it out.
func (u ToolUse) Why() string {
	if !whyTools[u.Name] {
		return ""
	}
	s, _ := u.Input[WhyKey].(string)
	return strings.TrimSpace(s)
}

// Args is Input without the presentation args, for running, validating and
// comparing a call. The same map when there is nothing to strip.
func (u ToolUse) Args() map[string]any {
	has := func(key string, tools map[string]bool) bool {
		_, ok := u.Input[key]
		return ok && tools[u.Name]
	}
	label, why := has(CallLabelKey, labeledTools), has(WhyKey, whyTools)
	if !label && !why {
		return u.Input
	}
	out := maps.Clone(u.Input)
	if label {
		delete(out, CallLabelKey)
	}
	if why {
		delete(out, WhyKey)
	}
	return out
}
