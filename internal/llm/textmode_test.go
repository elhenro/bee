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
