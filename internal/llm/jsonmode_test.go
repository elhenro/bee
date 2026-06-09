package llm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func jmTools() []ToolSpec {
	return []ToolSpec{
		{Name: "bash", PromptSnippet: "run shell", Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"command": map[string]any{"type": "string"}},
			"required":   []any{"command"},
		}},
		{Name: "read", Description: "read a file. Args: path."},
	}
}

func TestJSONMode_StripsToolsSetsSchemaInjectsInstruction(t *testing.T) {
	inner := &fakeProvider{name: "x", events: []Event{{Type: EventDone, StopReason: "stop"}}}
	p := NewJSONMode(inner)
	ch, err := p.Stream(context.Background(), Request{System: "be brief", Tools: jmTools()})
	if err != nil {
		t.Fatal(err)
	}
	drainTM(ch)

	if inner.gotReq.Tools != nil {
		t.Fatalf("inner saw Tools, expected nil; got %+v", inner.gotReq.Tools)
	}
	if inner.gotReq.ResponseSchema == nil {
		t.Fatal("ResponseSchema not set")
	}
	branches, ok := inner.gotReq.ResponseSchema["anyOf"].([]any)
	if !ok || len(branches) != 2 {
		t.Fatalf("expected 2 anyOf branches (done + tool), got %v", inner.gotReq.ResponseSchema)
	}
	b0, _ := json.Marshal(branches[0])
	if !strings.Contains(string(b0), `"required":["done"]`) {
		t.Fatalf("terminal branch must require done: %s", b0)
	}
	b1, _ := json.Marshal(branches[1])
	if !strings.Contains(string(b1), `"enum":["bash","read"]`) {
		t.Fatalf("tool branch missing enum pin in advert order: %s", b1)
	}
	// args stay untyped (strict nested arg schemas flip small models to the
	// done branch); optional say rides along, required stays tool+args.
	if !strings.Contains(string(b1), `"args":{"type":"object"}`) {
		t.Fatalf("args must be untyped on the tool branch: %s", b1)
	}
	if !strings.Contains(string(b1), `"say":{"type":"string"}`) {
		t.Fatalf("tool branch missing optional say: %s", b1)
	}
	if !strings.Contains(string(b1), `"required":["tool","args"]`) {
		t.Fatalf("say must not be required on tool branch: %s", b1)
	}
	// xgrammar permits only the DECLARED property order at sampling time, so
	// the wire order must stay say,tool,args — alphabetical (args first)
	// forces tool calls to open with {"args": and breaks small models.
	s1 := string(b1)
	if !(strings.Index(s1, `"say"`) < strings.Index(s1, `"tool"`) && strings.Index(s1, `"tool"`) < strings.Index(s1, `"args"`)) {
		t.Fatalf("tool branch property order must be say,tool,args: %s", s1)
	}
	if !strings.Contains(inner.gotReq.System, "be brief") {
		t.Fatalf("existing system prompt dropped: %s", inner.gotReq.System)
	}
	if !strings.Contains(inner.gotReq.System, "- bash(command:string): run shell") {
		t.Fatalf("bash advert missing: %s", inner.gotReq.System)
	}
	if !strings.Contains(inner.gotReq.System, "- read: read a file") {
		t.Fatalf("read description fallback failed: %s", inner.gotReq.System)
	}
}

func TestJSONMode_NoToolsPassthrough(t *testing.T) {
	inner := &fakeProvider{events: []Event{{Type: EventDone, StopReason: "stop"}}}
	p := NewJSONMode(inner)
	ch, _ := p.Stream(context.Background(), Request{System: "summarize"})
	drainTM(ch)
	if inner.gotReq.ResponseSchema != nil {
		t.Fatal("side-LLM call must not be constrained")
	}
	if inner.gotReq.System != "summarize" {
		t.Fatalf("system mutated on passthrough: %q", inner.gotReq.System)
	}
}

func TestJSONMode_ParsesToolEnvelope(t *testing.T) {
	inner := &fakeProvider{events: []Event{
		{Type: EventTextDelta, Delta: `{"tool":"bash","args"`},
		{Type: EventTextDelta, Delta: `:{"command":"ls"}}`},
		{Type: EventDone, StopReason: "stop"},
	}}
	p := NewJSONMode(inner)
	ch, _ := p.Stream(context.Background(), Request{Tools: jmTools()})
	tools, text, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "bash" {
		t.Fatalf("expected one bash call, got %v", tools)
	}
	if tools[0].Input["command"] != "ls" {
		t.Fatalf("args lost: %v", tools[0].Input)
	}
	if text != "" {
		t.Fatalf("tool turn leaked text: %q", text)
	}
}

func TestJSONMode_ParsesDoneEnvelope(t *testing.T) {
	inner := &fakeProvider{events: []Event{
		{Type: EventTextDelta, Delta: `{"done":"all tests pass"}`},
		{Type: EventDone, StopReason: "stop"},
	}}
	p := NewJSONMode(inner)
	ch, _ := p.Stream(context.Background(), Request{Tools: jmTools()})
	tools, text, _ := collect(ch)
	if len(tools) != 0 {
		t.Fatalf("done turn produced tool calls: %v", tools)
	}
	if text != "all tests pass" {
		t.Fatalf("done text mangled: %q", text)
	}
}

// legacy say-only shape (degraded servers, older transcripts) still parses.
func TestJSONMode_ParsesLegacySayEnvelope(t *testing.T) {
	inner := &fakeProvider{events: []Event{
		{Type: EventTextDelta, Delta: `{"say":"all tests pass"}`},
		{Type: EventDone, StopReason: "stop"},
	}}
	p := NewJSONMode(inner)
	ch, _ := p.Stream(context.Background(), Request{Tools: jmTools()})
	tools, text, _ := collect(ch)
	if len(tools) != 0 {
		t.Fatalf("say turn produced tool calls: %v", tools)
	}
	if text != "all tests pass" {
		t.Fatalf("say text mangled: %q", text)
	}
}

func TestJSONMode_ParsesCombinedSayAndTool(t *testing.T) {
	inner := &fakeProvider{events: []Event{
		{Type: EventTextDelta, Delta: `{"say":"checking tests","tool":"bash","args":{"command":"go test"}}`},
		{Type: EventDone, StopReason: "stop"},
	}}
	p := NewJSONMode(inner)
	ch, _ := p.Stream(context.Background(), Request{Tools: jmTools()})
	tools, text, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "bash" {
		t.Fatalf("combined envelope lost the tool call: %v", tools)
	}
	if text != "checking tests" {
		t.Fatalf("combined envelope lost the say note: %q", text)
	}
}

func TestJSONMode_FencedAndPaddedOutput(t *testing.T) {
	inner := &fakeProvider{events: []Event{
		{Type: EventTextDelta, Delta: "```json\n{\"tool\":\"read\",\"args\":{\"path\":\"a.go\"}}\n```"},
		{Type: EventDone, StopReason: "stop"},
	}}
	p := NewJSONMode(inner)
	ch, _ := p.Stream(context.Background(), Request{Tools: jmTools()})
	tools, _, _ := collect(ch)
	if len(tools) != 1 || tools[0].Name != "read" {
		t.Fatalf("fenced envelope not parsed: %v", tools)
	}
}

func TestJSONMode_MalformedFallsBackToRawText(t *testing.T) {
	inner := &fakeProvider{events: []Event{
		{Type: EventTextDelta, Delta: `I will now run the tests.`},
		{Type: EventDone, StopReason: "stop"},
	}}
	p := NewJSONMode(inner)
	ch, _ := p.Stream(context.Background(), Request{Tools: jmTools()})
	tools, text, _ := collect(ch)
	if len(tools) != 0 {
		t.Fatalf("prose produced tool calls: %v", tools)
	}
	if text != "I will now run the tests." {
		t.Fatalf("raw fallback lost: %q", text)
	}
}

func TestJSONMode_UnionSchemaShape(t *testing.T) {
	schema := buildUnionSchema([]ToolSpec{{Name: "noop"}})
	b, _ := json.Marshal(schema)
	if !strings.Contains(string(b), `"args":{"type":"object"}`) {
		t.Fatalf("args not untyped: %s", b)
	}
	if !strings.Contains(string(b), `"enum":["noop"]`) {
		t.Fatalf("tool enum missing: %s", b)
	}
}
