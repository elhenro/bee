package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

// fakeProvider records the Request it was given and emits a scripted event
// list. lets us assert what the wrapper passes through.
type fakeProvider struct {
	name   string
	gotReq Request
	events []Event
	err    error
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	f.gotReq = req
	if f.err != nil {
		return nil, f.err
	}
	out := make(chan Event, len(f.events)+1)
	for _, e := range f.events {
		out <- e
	}
	close(out)
	return out, nil
}

func newKnownTools() []ToolSpec {
	return []ToolSpec{
		{Name: "write", PromptSnippet: "write a file"},
		{Name: "edit_diff", Description: "string replace in a file. Args: path, old, new."},
		{Name: "shell", PromptSnippet: "run shell"},
	}
}

func TestTextMode_StripsToolsAndInjects(t *testing.T) {
	inner := &fakeProvider{name: "x", events: []Event{{Type: EventDone, StopReason: "stop"}}}
	p := NewTextMode(inner, TextModeOptions{})
	req := Request{System: "be brief", Tools: newKnownTools()}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	drainTM(ch)

	if inner.gotReq.Tools != nil {
		t.Fatalf("inner saw Tools, expected nil; got %+v", inner.gotReq.Tools)
	}
	if !strings.Contains(inner.gotReq.System, "## Tools (text format)") {
		t.Fatalf("instruction block missing: %s", inner.gotReq.System)
	}
	if !strings.Contains(inner.gotReq.System, "- write: write a file") {
		t.Fatalf("write tool not listed: %s", inner.gotReq.System)
	}
	if !strings.Contains(inner.gotReq.System, "- edit_diff: string replace in a file") {
		t.Fatalf("edit_diff description fallback failed: %s", inner.gotReq.System)
	}
	if !strings.Contains(inner.gotReq.System, "be brief") {
		t.Fatalf("existing system prompt dropped: %s", inner.gotReq.System)
	}
}

func TestTextMode_ParsesSingleCall(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `<write>{"path":"x","content":"y"}</write>`},
			{Type: EventDone, StopReason: "stop"},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 {
		t.Fatalf("calls: got %d, want 1", len(tools))
	}
	if tools[0].Name != "write" || tools[0].Input["path"] != "x" || tools[0].Input["content"] != "y" {
		t.Fatalf("call: %+v", tools[0])
	}
}

func TestTextMode_ParsesTwoSequential(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `<write>{"path":"a","content":"1"}</write>` + "\n" +
				`<shell>{"command":"ls"}</shell>`},
			{Type: EventDone, StopReason: "stop"},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 2 {
		t.Fatalf("calls: got %d, want 2", len(tools))
	}
	if tools[0].Name != "write" || tools[1].Name != "shell" {
		t.Fatalf("order: %+v", tools)
	}
	if tools[1].Input["command"] != "ls" {
		t.Fatalf("shell args: %+v", tools[1].Input)
	}
}

func TestTextMode_MissingCloseTag(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `intro <write>{"path":"x","content":"y"}`},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 {
		t.Fatalf("calls: got %d, want 1", len(tools))
	}
	if tools[0].Input["path"] != "x" {
		t.Fatalf("input: %+v", tools[0].Input)
	}
}

func TestTextMode_LenientTrailingComma(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `<write>{"path":"x","content":"y",}</write>`},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 {
		t.Fatalf("calls: got %d, want 1", len(tools))
	}
	if _, bad := tools[0].Input["_parse_error"]; bad {
		t.Fatalf("trailing comma should have been repaired: %+v", tools[0].Input)
	}
	if tools[0].Input["path"] != "x" {
		t.Fatalf("repaired input wrong: %+v", tools[0].Input)
	}
}

func TestTextMode_BadJSONSurfacesParseError(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `<write>this is not json at all</write>`},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 {
		t.Fatalf("calls: got %d, want 1", len(tools))
	}
	if _, ok := tools[0].Input["_parse_error"]; !ok {
		t.Fatalf("expected _parse_error marker, got %+v", tools[0].Input)
	}
}

func TestTextMode_CleansToolBlocksFromText(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: "before\n<write>{\"path\":\"x\",\"content\":\"y\"}</write>\nafter"},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	_, text, _ := collect(ch)
	if strings.Contains(text, "<write>") {
		t.Fatalf("tool block leaked into text: %q", text)
	}
	if !strings.Contains(text, "before") || !strings.Contains(text, "after") {
		t.Fatalf("surrounding prose dropped: %q", text)
	}
}

func TestTextMode_StripsDSMLOuterMarkup(t *testing.T) {
	// text-mode providers sometimes append `</｜DSML｜invoke` after the
	// closing tool tag. that trailing markup is consumed by the outer
	// tag-removal sweep and never reaches arg parsing.
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `<shell>{"command":"ls -la"}</shell></｜DSML｜invoke`},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 {
		t.Fatalf("calls: got %d, want 1", len(tools))
	}
	if got := tools[0].Input["command"]; got != "ls -la" {
		t.Fatalf("command: %q (input=%+v)", got, tools[0].Input)
	}
}

func TestTextMode_PreservesInnerLeakTags(t *testing.T) {
	// inner `</parameter>` inside a quoted JSON string is legitimate user
	// content (e.g. test fixtures, source code, prompts) — it must round-
	// trip verbatim, NOT get stripped by the markup scrubber.
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `<write>{"path":"x","content":"func TestX() { return ` + "`" + `</parameter>` + "`" + ` }"}</write>`},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 {
		t.Fatalf("calls: got %d, want 1", len(tools))
	}
	got, _ := tools[0].Input["content"].(string)
	if !strings.Contains(got, `</parameter>`) {
		t.Fatalf("inner leak tag was stripped — destructive: %q", got)
	}
}

func TestTextMode_PassesThroughThinkingDeltas(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventThinkingDelta, Delta: "hmm"},
			{Type: EventTextDelta, Delta: `<shell>{"command":"pwd"}</shell>`},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, thinking := collect(ch)
	if thinking != "hmm" {
		t.Fatalf("thinking lost: %q", thinking)
	}
	if len(tools) != 1 || tools[0].Name != "shell" {
		t.Fatalf("tool: %+v", tools)
	}
}

// JSON fallback: model emits {"type":"shell","command":"ls"} instead of
// <shell>{...}</shell>. seen with small local models and big hosted
// reasoners that revert to native function-call JSON despite the XML hint.
func TestTextMode_ParsesJSONShape_TypeWithInlineArgs(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `{"type":"shell","command":"ls -la"}`},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, text, _ := collect(ch)
	if len(tools) != 1 {
		t.Fatalf("calls: got %d, want 1 (input=%q)", len(tools), text)
	}
	if tools[0].Name != "shell" {
		t.Fatalf("name: %q", tools[0].Name)
	}
	if tools[0].Input["command"] != "ls -la" {
		t.Fatalf("args: %+v", tools[0].Input)
	}
	if strings.Contains(text, "shell") {
		t.Fatalf("JSON block leaked: %q", text)
	}
}

// {"type":"<tool>","arguments":{...}} — OpenAI-flavored
func TestTextMode_ParsesJSONShape_TypeWithArguments(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `{"type":"shell","arguments":{"command":"pwd"}}`},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "shell" {
		t.Fatalf("tool: %+v", tools)
	}
	if tools[0].Input["command"] != "pwd" {
		t.Fatalf("args: %+v", tools[0].Input)
	}
}

// {"name":"<tool>","arguments":{...}} — Anthropic-flavored
func TestTextMode_ParsesJSONShape_NameWithArguments(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `{"name":"write","arguments":{"path":"a","content":"b"}}`},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "write" {
		t.Fatalf("tool: %+v", tools)
	}
	if tools[0].Input["path"] != "a" || tools[0].Input["content"] != "b" {
		t.Fatalf("args: %+v", tools[0].Input)
	}
}

// {"name":"<tool>","input":{...}} — also Anthropic-style
func TestTextMode_ParsesJSONShape_NameWithInput(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `{"name":"write","input":{"path":"a","content":"b"}}`},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "write" {
		t.Fatalf("tool: %+v", tools)
	}
	if tools[0].Input["path"] != "a" {
		t.Fatalf("args: %+v", tools[0].Input)
	}
}

// {"type":"function","function":{"name":"<tool>","arguments":"<json-string>"}}
// — OpenAI raw shape with arguments as a JSON string.
func TestTextMode_ParsesJSONShape_OpenAIFunction(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `{"type":"function","function":{"name":"shell","arguments":"{\"command\":\"git status\"}"}}`},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "shell" {
		t.Fatalf("tool: %+v", tools)
	}
	if tools[0].Input["command"] != "git status" {
		t.Fatalf("args: %+v", tools[0].Input)
	}
}

// JSON inside a ```json fence — strip the fence with the block.
func TestTextMode_ParsesJSONShape_InsideCodeFence(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: "intro\n```json\n{\"type\":\"shell\",\"command\":\"ls\"}\n```\noutro"},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, text, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "shell" {
		t.Fatalf("tool: %+v", tools)
	}
	if strings.Contains(text, "```") || strings.Contains(text, `"type"`) {
		t.Fatalf("fence/JSON leaked: %q", text)
	}
	if !strings.Contains(text, "intro") || !strings.Contains(text, "outro") {
		t.Fatalf("surrounding prose dropped: %q", text)
	}
}

// Unknown tool name in JSON shape — leave verbatim, no synthetic call.
func TestTextMode_IgnoresJSONShapeUnknownTool(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `{"type":"browseMemory","query":"x"}`},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, text, _ := collect(ch)
	if len(tools) != 0 {
		t.Fatalf("unknown tool name should not create a call, got %+v", tools)
	}
	if !strings.Contains(text, "browseMemory") {
		t.Fatalf("unknown JSON should round-trip in text, got %q", text)
	}
}

// qwen3 / hermes wrapper: `<tool_call>{"name":...,"arguments":{...}}</tool_call>`.
// the wrapper is stripped, then the JSON extractor picks up the bare envelope.
func TestTextMode_ParsesHermesToolCallWrapper(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: "<tool_call>\n{\"name\":\"shell\",\"arguments\":{\"command\":\"ls -la\"}}\n</tool_call>"},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, text, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "shell" {
		t.Fatalf("tool: %+v (text=%q)", tools, text)
	}
	if tools[0].Input["command"] != "ls -la" {
		t.Fatalf("args: %+v", tools[0].Input)
	}
	if strings.Contains(text, "tool_call") {
		t.Fatalf("hermes wrapper leaked: %q", text)
	}
}

// qwen3 xml variant: `<function=NAME><parameter=K>V</parameter></function>`,
// optionally inside a <tool_call> wrapper. seen in the wild from qwen3-35B-A3B
// when textmode is forced and the model falls back to its chat-template's
// native shape.
func TestTextMode_ParsesHermesFunctionXML(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: "<tool_call>\n<function=shell>\n<parameter=command>git status --short</parameter>\n</function>\n</tool_call>"},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, text, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "shell" {
		t.Fatalf("tool: %+v (text=%q)", tools, text)
	}
	if tools[0].Input["command"] != "git status --short" {
		t.Fatalf("args: %+v", tools[0].Input)
	}
}

// `<function=NAME>` without the outer `<tool_call>` wrapper still parses.
func TestTextMode_ParsesHermesFunctionXMLUnwrapped(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: "<function=write>\n<parameter=path>x</parameter>\n<parameter=content>hi</parameter>\n</function>"},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "write" {
		t.Fatalf("tool: %+v", tools)
	}
	if tools[0].Input["path"] != "x" || tools[0].Input["content"] != "hi" {
		t.Fatalf("args: %+v", tools[0].Input)
	}
}

// some MoE models emit the DSML envelope as plain content (not native openai
// tool_calls). bee previously stripped DSML tokens as markup-leak junk on the
// native tool_call path, but content-side envelopes never reached that path —
// they fell through unparsed and the call died. parser now turns the envelope
// into the canonical <NAME>{json}</NAME> shape so the tool dispatches.
func TestTextMode_ParsesDSMLEnvelope(t *testing.T) {
	body := "<｜｜DSML｜｜tool_calls>\n" +
		"<｜｜DSML｜｜invoke name=\"shell\">\n" +
		"<｜｜DSML｜｜parameter name=\"command\" string=\"true\">grep foo .</｜｜DSML｜｜parameter>\n" +
		"</｜｜DSML｜｜invoke>\n" +
		"</｜｜DSML｜｜tool_calls>"
	inner := &fakeProvider{
		events: []Event{{Type: EventTextDelta, Delta: body}, {Type: EventDone}},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, text, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "shell" {
		t.Fatalf("tool: %+v (text=%q)", tools, text)
	}
	if tools[0].Input["command"] != "grep foo ." {
		t.Fatalf("args: %+v", tools[0].Input)
	}
	if strings.Contains(text, "DSML") {
		t.Fatalf("DSML leaked into text: %q", text)
	}
}

// single-pipe DSML variant — some templates emit `｜DSML｜` instead of `｜｜DSML｜｜`.
func TestTextMode_ParsesDSMLSinglePipe(t *testing.T) {
	body := "<｜DSML｜invoke name=\"write\">\n" +
		"<｜DSML｜parameter name=\"path\">/tmp/x</｜DSML｜parameter>\n" +
		"<｜DSML｜parameter name=\"content\">hi</｜DSML｜parameter>\n" +
		"</｜DSML｜invoke>"
	inner := &fakeProvider{
		events: []Event{{Type: EventTextDelta, Delta: body}, {Type: EventDone}},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "write" {
		t.Fatalf("tool: %+v", tools)
	}
	if tools[0].Input["path"] != "/tmp/x" || tools[0].Input["content"] != "hi" {
		t.Fatalf("args: %+v", tools[0].Input)
	}
}

// stop sequence cuts the stream past the first param close, so neither
// </invoke> nor </tool_calls> ever arrives. parser must consume the rest of
// the buffer as the body or the call is lost.
func TestTextMode_ParsesDSMLEnvelopeNoClose(t *testing.T) {
	body := "<｜｜DSML｜｜invoke name=\"shell\">\n" +
		"<｜｜DSML｜｜parameter name=\"command\" string=\"true\">ls</｜｜DSML｜｜parameter>"
	inner := &fakeProvider{
		events: []Event{{Type: EventTextDelta, Delta: body}, {Type: EventDone}},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "shell" {
		t.Fatalf("tool: %+v", tools)
	}
	if tools[0].Input["command"] != "ls" {
		t.Fatalf("args: %+v", tools[0].Input)
	}
}

// `<tool_call><tool_name>NAME</tool_name>{json}</tool_call>` is yet another
// shape seen from qwen3-A3B when it reads the advert's placeholder example
// too literally. Args follow as a bare JSON object. Stop sequence still cuts
// after the args close brace so </tool_call> may be missing too.
func TestTextMode_ParsesToolNameTagVariant(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: "<tool_call>\n<tool_name>shell</tool_name>\n{\"command\":\"ls\"}\n</tool_call>"},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "shell" {
		t.Fatalf("tool: %+v", tools)
	}
	if tools[0].Input["command"] != "ls" {
		t.Fatalf("args: %+v", tools[0].Input)
	}
}

// `<tool_name>NAME</tool_name>{json}` without the outer <tool_call> wrapper.
func TestTextMode_ParsesToolNameTagUnwrapped(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: "<tool_name>write</tool_name>{\"path\":\"x\",\"content\":\"hi\"}"},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "write" {
		t.Fatalf("tool: %+v", tools)
	}
	if tools[0].Input["path"] != "x" || tools[0].Input["content"] != "hi" {
		t.Fatalf("args: %+v", tools[0].Input)
	}
}

// qwen3 in the wild closes <function=NAME> with </NAME>, not </function>.
// also, the textmode stop sequence (`</NAME>`) chops the stream the moment
// that close lands, so we never see </function> or </tool_call> past it.
// regression: this caused the agent to hang on edit/shell calls because the
// envelope stayed raw and no tool was extracted.
func TestTextMode_ParsesHermesFunctionXMLNameClose(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: "<tool_call>\n<function=write>\n<parameter=path>x</parameter>\n<parameter=content>hi</parameter>\n</write>"},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "write" {
		t.Fatalf("tool: %+v", tools)
	}
	if tools[0].Input["path"] != "x" || tools[0].Input["content"] != "hi" {
		t.Fatalf("args: %+v", tools[0].Input)
	}
}

// stop sequence cut: model emits `<function=NAME>...<parameter=...>...` and
// nothing past the first parameter body close. parser must consume rest of
// buffer as body or the call is lost and the loop stalls forever.
func TestTextMode_ParsesHermesFunctionXMLNoClose(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: "<tool_call>\n<function=shell>\n<parameter=command>ls -la</parameter>"},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "shell" {
		t.Fatalf("tool: %+v", tools)
	}
	if tools[0].Input["command"] != "ls -la" {
		t.Fatalf("args: %+v", tools[0].Input)
	}
}

// schema parameter names get surfaced in the advert so the model uses the
// real keys instead of guessing (the root regression in 333f7bb: tiny profile
// gained xml format but advert dropped param hints, so qwen3 emitted
// `{"args":{"cmd":"..."}}` for bash and the tool failed on missing `command`).
func TestTextMode_AdvertIncludesSchemaParams(t *testing.T) {
	inner := &fakeProvider{events: []Event{{Type: EventDone}}}
	p := NewTextMode(inner, TextModeOptions{})
	tools := []ToolSpec{
		{
			Name:          "shell",
			PromptSnippet: "run shell",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string"},
					"cwd":     map[string]any{"type": "string"},
				},
				"required": []string{"command"},
			},
		},
	}
	ch, _ := p.Stream(context.Background(), Request{Tools: tools})
	drainTM(ch)
	sys := inner.gotReq.System
	if !strings.Contains(sys, "command:string") {
		t.Fatalf("required param `command` not surfaced in advert: %s", sys)
	}
	if !strings.Contains(sys, "[cwd:string]") {
		t.Fatalf("optional param `cwd` not bracketed in advert: %s", sys)
	}
}

// Plain JSON object that isn't a tool call — round-trip verbatim.
func TestTextMode_LeavesNonToolJSONIntact(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `here's the config: {"port":8080,"host":"x"} done`},
			{Type: EventDone},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, text, _ := collect(ch)
	if len(tools) != 0 {
		t.Fatalf("non-tool JSON triggered a call: %+v", tools)
	}
	if !strings.Contains(text, `"port":8080`) {
		t.Fatalf("non-tool JSON was stripped: %q", text)
	}
}

// qwen3 / hermes models sometimes echo the same tool call twice in one
// turn (chat-template artifact). dedup must collapse identical calls
// (same name + args) to one dispatch.
func TestTextMode_DedupesDuplicateCalls(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `<shell>{"command":"ls"}</shell>` + "\n" +
				`<shell>{"command":"ls"}</shell>`},
			{Type: EventDone, StopReason: "stop"},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 {
		t.Fatalf("dedup failed: got %d calls, want 1", len(tools))
	}
	if tools[0].Name != "shell" || tools[0].Input["command"] != "ls" {
		t.Fatalf("wrong call survived: %+v", tools[0])
	}
}

// distinct calls with same name but different args must NOT collapse.
func TestTextMode_DoesNotDedupeDistinctArgs(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `<shell>{"command":"ls"}</shell>` + "\n" +
				`<shell>{"command":"pwd"}</shell>`},
			{Type: EventDone, StopReason: "stop"},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 2 {
		t.Fatalf("over-deduped: got %d calls, want 2", len(tools))
	}
}

// dedup spans incremental + final passes: same call appearing first via the
// streaming fast-path and again at EventDone (e.g. provider re-delivers via
// native tool_use channel) must only fire once.
func TestTextMode_DedupesAcrossIncrementalAndNative(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `<shell>{"command":"ls"}</shell>`},
			{Type: EventToolUse, ToolUse: &types.ToolUse{
				ID:    "native_1",
				Name:  "shell",
				Input: map[string]any{"command": "ls"},
			}},
			{Type: EventDone, StopReason: "stop"},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 {
		t.Fatalf("cross-channel dedup failed: got %d calls, want 1", len(tools))
	}
}

// advert must explicitly endorse the hermes xml shape so qwen3-trained
// models don't fight their training. also must warn against markdown fences.
func TestTextMode_AdvertEndorsesHermesAndWarnsFences(t *testing.T) {
	inner := &fakeProvider{events: []Event{{Type: EventDone}}}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	drainTM(ch)
	sys := inner.gotReq.System
	if !strings.Contains(sys, "<function=NAME>") {
		t.Fatalf("hermes xml shape not endorsed in advert: %s", sys)
	}
	if !strings.Contains(sys, "markdown fences") {
		t.Fatalf("markdown-fence warning missing: %s", sys)
	}
}

// malformed hermes envelope with stripped leading `<` (qwen3-A3B with
// markup-stripped output, observed in the wild). parser can't extract a
// real call; nudge path must synthesize a _parse_error tool use so the loop
// surfaces a diagnostic instead of silently ending the turn.
func TestTextMode_NudgesOnMalformedFunctionEnvelope(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: "function=bash>\n<parameter=command>\nls -la\n</parameter>\n</function>\n</tool_call>"},
			{Type: EventDone, StopReason: "stop"},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, text, _ := collect(ch)
	if len(tools) != 1 {
		t.Fatalf("expected 1 nudge tool use, got %d", len(tools))
	}
	if tools[0].Name != "bash" {
		t.Fatalf("expected name inferred from `function=bash>`, got %q", tools[0].Name)
	}
	if pe, _ := tools[0].Input["_parse_error"].(string); pe == "" {
		t.Fatalf("expected _parse_error nudge, got input %+v", tools[0].Input)
	}
	if text != "" {
		t.Fatalf("malformed prose should be suppressed when nudging, got %q", text)
	}
}

// stray <tool_call> envelope with no parseable body — name is unknown but
// nudge must still fire so the loop tells the model to retry.
func TestTextMode_NudgesOnStrayToolCallEnvelope(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: "<tool_call>\nuhh\n</tool_call>"},
			{Type: EventDone, StopReason: "stop"},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 {
		t.Fatalf("expected nudge tool use, got %d", len(tools))
	}
	if tools[0].Name != "unknown" {
		t.Fatalf("expected unknown-name nudge, got %q", tools[0].Name)
	}
	if _, ok := tools[0].Input["_parse_error"].(string); !ok {
		t.Fatalf("expected _parse_error key in nudge args: %+v", tools[0].Input)
	}
}

// plain prose with angle brackets but no envelope markers must NOT trigger
// the nudge — false positives spam the model with bogus retries.
func TestTextMode_NoNudgeOnInnocentProse(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: "use the <kbd>Tab</kbd> key, then check if x > 5 and y < 10"},
			{Type: EventDone, StopReason: "stop"},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, text, _ := collect(ch)
	if len(tools) != 0 {
		t.Fatalf("false-positive nudge on innocent prose: %+v", tools)
	}
	if !strings.Contains(text, "Tab") {
		t.Fatalf("prose was eaten: %q", text)
	}
}

// when a real call DOES extract, the nudge path must stay silent even if
// the same text also contains envelope markers (e.g. mixed valid + stray).
func TestTextMode_NoNudgeWhenRealCallExtracted(t *testing.T) {
	inner := &fakeProvider{
		events: []Event{
			{Type: EventTextDelta, Delta: `<shell>{"command":"ls"}</shell>` + "\nleftover </tool_call>"},
			{Type: EventDone, StopReason: "stop"},
		},
	}
	p := NewTextMode(inner, TextModeOptions{})
	ch, _ := p.Stream(context.Background(), Request{Tools: newKnownTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 {
		t.Fatalf("expected only the real call, got %d: %+v", len(tools), tools)
	}
	if _, isErr := tools[0].Input["_parse_error"]; isErr {
		t.Fatalf("real call should not carry _parse_error: %+v", tools[0].Input)
	}
}

// drainTM reads all events to completion.
func drainTM(ch <-chan Event) {
	for range ch {
	}
}

// collect splits a stream into tool uses, joined text deltas, and joined
// thinking deltas. blocks until channel closes.
func collect(ch <-chan Event) ([]*types.ToolUse, string, string) {
	var tools []*types.ToolUse
	var text, think strings.Builder
	for ev := range ch {
		switch ev.Type {
		case EventToolUse:
			tools = append(tools, ev.ToolUse)
		case EventTextDelta:
			text.WriteString(ev.Delta)
		case EventThinkingDelta:
			think.WriteString(ev.Delta)
		}
	}
	return tools, text.String(), think.String()
}
