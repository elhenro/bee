package wire

import "testing"

func tcDelta(idx int, id, name, args string) StreamToolCall {
	return StreamToolCall{Index: idx, ID: id, Function: StreamFunctionDelta{Name: name, Arguments: args}}
}

func TestToolCallAccumulatorSplitsCollidingIndex(t *testing.T) {
	a := NewToolCallAccumulator()
	a.Apply([]StreamToolCall{tcDelta(0, "a", "read", `{"p":"1"}`)})
	a.Apply([]StreamToolCall{tcDelta(0, "b", "read", `{"p":"2"}`)})
	calls, _ := a.Finalize()
	if len(calls) != 2 || calls[0].ID != "a" || calls[1].ID != "b" {
		t.Fatalf("distinct ids on one index must stay separate, got %+v", calls)
	}
	if calls[1].Input["p"] != "2" || calls[0].ParseError != "" || calls[1].ParseError != "" {
		t.Errorf("args merged: %+v", calls)
	}
}

func TestToolCallAccumulatorContinuationKeepsSlot(t *testing.T) {
	a := NewToolCallAccumulator()
	a.Apply([]StreamToolCall{tcDelta(0, "a", "read", `{"p":`)})
	a.Apply([]StreamToolCall{tcDelta(0, "", "", `"1"}`)})
	a.Apply([]StreamToolCall{tcDelta(0, "a", "", "")}) // repeated id is not a new call
	calls, _ := a.Finalize()
	if len(calls) != 1 || calls[0].Input["p"] != "1" {
		t.Errorf("got %+v", calls)
	}
}

func TestFinalizeTruncatedDoesNotCloseJSON(t *testing.T) {
	cut := `{"path":"a.txt","content":"half a fi`
	a := NewToolCallAccumulator()
	a.Apply([]StreamToolCall{tcDelta(0, "a", "write", cut)})
	a.MarkTruncated()
	calls, _ := a.Finalize()
	if calls[0].ParseError == "" || len(calls[0].Input) != 0 {
		t.Errorf("truncated args must fail, got %+v", calls[0])
	}
	if calls[0].RawArgs != cut {
		t.Errorf("raw args lost: %q", calls[0].RawArgs)
	}

	// without truncation the closing repair still applies
	b := NewToolCallAccumulator()
	b.Apply([]StreamToolCall{tcDelta(0, "a", "write", `{"path":"a.txt"`)})
	calls, _ = b.Finalize()
	if calls[0].ParseError != "" || calls[0].Input["path"] != "a.txt" {
		t.Errorf("clean-stream repair regressed: %+v", calls[0])
	}
}

func TestFinalizeTruncatedStillTrimsTrailingJunk(t *testing.T) {
	a := NewToolCallAccumulator()
	a.Apply([]StreamToolCall{tcDelta(0, "a", "read", `{"p":"1"}}`)})
	a.MarkTruncated()
	calls, _ := a.Finalize()
	if calls[0].ParseError != "" || calls[0].Input["p"] != "1" {
		t.Errorf("got %+v", calls[0])
	}
}

// legit content must survive arg sanitising: trailing newlines and fullwidth
// bars used as separators are user data, only leaked special tokens go.
func TestFinalize_PreservesUserContent(t *testing.T) {
	cases := []struct{ name, args, key, want string }{
		{"trailing newline", `{"content":"all:\n\techo hi\n"}`, "content", "all:\n\techo hi\n"},
		{"trailing spaces", `{"new_string":"x  \n"}`, "new_string", "x  \n"},
		{"fullwidth separators", `{"content":"ホーム｜会社概要｜お問い合わせ"}`, "content", "ホーム｜会社概要｜お問い合わせ"},
		{"latin fullwidth bars", `{"content":"a｜b｜c"}`, "content", "a｜b｜c"},
		{"trailing leak still removed", "{\"command\":\"ls\"}</｜DSML｜invoke", "command", "ls"},
		{"bare token still removed", `{"command":"ls ｜DSML｜"}`, "command", "ls"},
	}
	for _, c := range cases {
		acc := NewToolCallAccumulator()
		acc.Apply([]StreamToolCall{{Index: 0, ID: "a", Function: StreamFunctionDelta{Name: "write", Arguments: c.args}}})
		out, _ := acc.Finalize()
		if got := out[0].Input[c.key]; got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}
