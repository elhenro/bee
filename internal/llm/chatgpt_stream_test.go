package llm

import (
	"context"
	"strings"
	"testing"
)

func collectEvents(run func(chan<- Event)) []Event {
	out := make(chan Event, 64)
	run(out)
	var evs []Event
	for ev := range out {
		evs = append(evs, ev)
	}
	return evs
}

func responsesStream(t *testing.T, events ...string) []Event {
	t.Helper()
	body := "data: " + strings.Join(events, "\ndata: ") + "\n"
	p := &ChatGPTProvider{cfg: ChatGPTConfig{Name: "p"}}
	return collectEvents(func(out chan<- Event) { p.streamLoop(context.Background(), sseResp(body), out) })
}

// arguments.done and output_item.done carry the authoritative final arguments,
// replacing the accumulated deltas; the item fills in a missing name and id.
func TestResponsesStream_FinalArgumentsReplaceDeltas(t *testing.T) {
	evs := responsesStream(t,
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc1"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc1","delta":"{\"a\":"}`,
		`{"type":"response.function_call_arguments.done","item_id":"fc1","arguments":"{\"a\":1}"}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc1","call_id":"c1","name":"bash","arguments":"{\"b\":2}"}}`,
	)
	if len(evs) != 2 || evs[0].Type != EventToolUse {
		t.Fatalf("events = %+v", evs)
	}
	use := evs[0].ToolUse
	if use.ID != "c1" || use.Name != "bash" || use.Input["b"] != float64(2) || len(use.Input) != 1 {
		t.Errorf("tool use = %+v", use)
	}
}

func TestResponsesStream_IncompleteMaxTokensIsLength(t *testing.T) {
	evs := responsesStream(t, `{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":1,"output_tokens":2}}}`)
	done := evs[len(evs)-1]
	if done.Type != EventDone || done.StopReason != "length" || done.Usage == nil || done.Usage.OutputTokens != 2 {
		t.Errorf("done = %+v", done)
	}
}

func TestResponsesStream_ErrorEvents(t *testing.T) {
	cases := map[string]string{
		`{"type":"error","code":"rate_limit"}`:                                      "provider p: rate_limit",
		`{"type":"error"}`:                                                          "provider p: responses stream error",
		`{"type":"response.failed","response":{"error":{"code":"boom"}}}`:           "boom",
		`{"type":"response.failed"}`:                                                "responses stream failed",
		`{"type":"response.failed","response":{"error":{"message":"nope"}}}`:        "nope",
		`{"type":"error","message":"bad"}`:                                          "provider p: bad",
		`{"type":"response.error","response":{"error":{"message":"m","code":"c"}}}`: "m",
	}
	for payload, want := range cases {
		evs := responsesStream(t, `{"type":"response.output_text.delta","delta":"x"}`, payload, `{"type":"response.output_text.delta","delta":"never"}`)
		last := evs[len(evs)-1]
		if len(evs) != 2 || last.Type != EventError || last.Err.Error() != want {
			t.Errorf("%s: events = %+v, want error %q after one delta", payload, evs, want)
		}
	}
}

func TestChatGPT_NonStreamLoop(t *testing.T) {
	body := `{"status":"completed","output":[` +
		`{"type":"message","content":[{"type":"output_text","text":"hi"}]},` +
		`{"type":"function_call","call_id":"c1","name":"bash","arguments":"{\"a\":1}"}],` +
		`"usage":{"input_tokens":3,"output_tokens":4}}`
	p := &ChatGPTProvider{}
	evs := collectEvents(func(out chan<- Event) { p.nonStreamLoop(context.Background(), sseResp(body), out) })
	if len(evs) != 3 || evs[0].Delta != "hi" || evs[1].ToolUse.ID != "c1" || evs[1].ToolUse.Input["a"] != float64(1) {
		t.Fatalf("events = %+v", evs)
	}
	if evs[2].Type != EventDone || evs[2].StopReason != "completed" || evs[2].Usage.InputTokens != 3 {
		t.Errorf("done = %+v", evs[2])
	}
	if evs := collectEvents(func(out chan<- Event) { p.nonStreamLoop(context.Background(), sseResp("not json"), out) }); len(evs) != 1 || evs[0].Type != EventError {
		t.Errorf("bad body events = %+v", evs)
	}
}
