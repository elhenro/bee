package mockprov

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

func TestParseEmptyFails(t *testing.T) {
	if _, err := Parse([]byte(`{"scenarios":[]}`)); err == nil {
		t.Fatal("expected error for empty scenarios")
	}
}

func TestMatcherAcceptsContains(t *testing.T) {
	req := llm.Request{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "please read foo.go"}}},
	}}
	if !(Matcher{Contains: "read foo"}).Accepts(req) {
		t.Fatal("contains match failed")
	}
	if (Matcher{Contains: "write"}).Accepts(req) {
		t.Fatal("non-matching contains should reject")
	}
}

func TestMatcherAny(t *testing.T) {
	req := llm.Request{}
	if !(Matcher{Any: true}).Accepts(req) {
		t.Fatal("Any should accept empty req")
	}
	if (Matcher{}).Accepts(req) {
		t.Fatal("zero matcher should reject")
	}
}

func TestMatcherRole(t *testing.T) {
	req := llm.Request{Messages: []types.Message{
		{Role: types.RoleTool, Content: []types.ContentBlock{
			{Type: types.BlockToolResult, Result: &types.ToolResult{Content: "result body"}},
		}},
	}}
	if !(Matcher{Role: "tool"}).Accepts(req) {
		t.Fatal("role match failed")
	}
	if (Matcher{Role: "user"}).Accepts(req) {
		t.Fatal("wrong role should reject")
	}
	if !(Matcher{Role: "tool", Contains: "result"}).Accepts(req) {
		t.Fatal("role+contains should both hold")
	}
}

func TestScriptedStreamLinear(t *testing.T) {
	f, err := Parse([]byte(`{"scenarios":[
		{"name":"first","match":{"contains":"hello"},
		 "events":[{"type":"text_delta","delta":"hi"},
		           {"type":"done","stop_reason":"stop","usage":{"input":1,"output":2}}]},
		{"name":"second","match":{"any":true},
		 "events":[{"type":"text_delta","delta":"again"},
		           {"type":"done","stop_reason":"stop"}]}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	p := NewScripted(f)
	if p.Name() != "mockprov" {
		t.Errorf("Name() = %q", p.Name())
	}

	// turn 1: must match "hello"
	req1 := llm.Request{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "hello there"}}},
	}}
	ch, err := p.Stream(context.Background(), req1)
	if err != nil {
		t.Fatalf("turn1 stream: %v", err)
	}
	evs1 := drain(ch)
	if len(evs1) != 2 || evs1[0].Type != llm.EventTextDelta || evs1[0].Delta != "hi" {
		t.Errorf("turn1 events wrong: %+v", evs1)
	}
	if evs1[1].Type != llm.EventDone || evs1[1].StopReason != "stop" {
		t.Errorf("turn1 done wrong: %+v", evs1[1])
	}
	if evs1[1].Usage == nil || evs1[1].Usage.InputTokens != 1 || evs1[1].Usage.OutputTokens != 2 {
		t.Errorf("turn1 usage wrong: %+v", evs1[1].Usage)
	}

	// turn 2: any matcher
	req2 := llm.Request{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "anything"}}},
	}}
	ch, err = p.Stream(context.Background(), req2)
	if err != nil {
		t.Fatalf("turn2 stream: %v", err)
	}
	evs2 := drain(ch)
	if len(evs2) != 2 || evs2[0].Delta != "again" {
		t.Errorf("turn2 events wrong: %+v", evs2)
	}

	if p.Remaining() != 0 {
		t.Errorf("expected fixture exhausted, remaining=%d", p.Remaining())
	}

	// turn 3: exhausted → error
	if _, err := p.Stream(context.Background(), req2); err == nil {
		t.Fatal("expected exhaustion error")
	} else if !strings.Contains(err.Error(), "exhausted") {
		t.Errorf("wrong exhaustion error: %v", err)
	}
}

func TestScriptedStreamMatcherMismatch(t *testing.T) {
	f, _ := Parse([]byte(`{"scenarios":[
		{"name":"strict","match":{"contains":"FOOBAR"},
		 "events":[{"type":"done","stop_reason":"stop"}]}
	]}`))
	p := NewScripted(f)
	req := llm.Request{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "anything else"}}},
	}}
	_, err := p.Stream(context.Background(), req)
	if err == nil {
		t.Fatal("expected matcher rejection error")
	}
	if !strings.Contains(err.Error(), "matcher rejected") {
		t.Errorf("wrong mismatch error: %v", err)
	}
}

func TestScriptedToolUseEvent(t *testing.T) {
	f, _ := Parse([]byte(`{"scenarios":[
		{"name":"call-read","match":{"any":true},
		 "events":[
		   {"type":"tool_use","tool":{"id":"toolu_x","name":"read","input":{"path":"/tmp/a"}}},
		   {"type":"done","stop_reason":"tool_use"}
		 ]}
	]}`))
	p := NewScripted(f)
	ch, err := p.Stream(context.Background(), llm.Request{})
	if err != nil {
		t.Fatal(err)
	}
	evs := drain(ch)
	if len(evs) != 2 {
		t.Fatalf("expected 2 events, got %d", len(evs))
	}
	if evs[0].Type != llm.EventToolUse || evs[0].ToolUse == nil {
		t.Fatalf("first event wrong: %+v", evs[0])
	}
	tu := evs[0].ToolUse
	if tu.ID != "toolu_x" || tu.Name != "read" {
		t.Errorf("tool_use fields wrong: %+v", tu)
	}
	if tu.Input["path"] != "/tmp/a" {
		t.Errorf("tool_use input wrong: %+v", tu.Input)
	}
	if evs[1].StopReason != "tool_use" {
		t.Errorf("done stop_reason wrong: %+v", evs[1])
	}
}

func TestScriptedErrorEvent(t *testing.T) {
	f, _ := Parse([]byte(`{"scenarios":[
		{"name":"boom","match":{"any":true},
		 "events":[{"type":"error","delta":"intentional"}]}
	]}`))
	p := NewScripted(f)
	ch, _ := p.Stream(context.Background(), llm.Request{})
	evs := drain(ch)
	if len(evs) != 1 || evs[0].Type != llm.EventError {
		t.Fatalf("expected single error event, got %+v", evs)
	}
	if !errors.Is(evs[0].Err, evs[0].Err) || evs[0].Err == nil {
		t.Fatalf("error event missing Err")
	}
}

func drain(ch <-chan llm.Event) []llm.Event {
	var out []llm.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}
