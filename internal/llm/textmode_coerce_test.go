package llm

import (
	"context"
	"reflect"
	"testing"
)

func coerceTools() []ToolSpec {
	return []ToolSpec{
		{Name: "write", Schema: map[string]any{"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		}}},
		{Name: "read", Schema: map[string]any{"properties": map[string]any{
			"path":  map[string]any{"type": "string"},
			"lines": map[string]any{"type": "integer"},
			"tags":  map[string]any{"type": "array"},
		}}},
	}
}

func tmInput(t *testing.T, body string) map[string]any {
	t.Helper()
	inner := &fakeProvider{events: []Event{{Type: EventTextDelta, Delta: body}, {Type: EventDone}}}
	ch, err := NewTextMode(inner, TextModeOptions{}).Stream(context.Background(), Request{Tools: coerceTools()})
	if err != nil {
		t.Fatal(err)
	}
	tools, text, _ := collect(ch)
	if len(tools) != 1 {
		t.Fatalf("want 1 call, got %+v (text=%q)", tools, text)
	}
	return tools[0].Input
}

// a file body that happens to be valid JSON is still a string for a
// string-typed param; typed params are decoded from their text.
func TestTextMode_HermesParamsFollowSchema(t *testing.T) {
	got := tmInput(t, "<function=write><parameter=path>package.json</parameter><parameter=content>{\"name\":\"x\"}</parameter></function>")
	if got["content"] != `{"name":"x"}` {
		t.Fatalf("json-looking content must stay a string: %#v", got["content"])
	}
	got = tmInput(t, "<function=write><parameter=path>VERSION</parameter><parameter=content>123</parameter></function>")
	if got["content"] != "123" {
		t.Fatalf("numeric-looking content must stay a string: %#v", got["content"])
	}
	got = tmInput(t, "<function=read><parameter=path>a</parameter><parameter=lines>40</parameter><parameter=tags>[\"a\",\"b\"]</parameter></function>")
	if got["lines"] != float64(40) || !reflect.DeepEqual(got["tags"], []any{"a", "b"}) {
		t.Fatalf("typed params must decode: %#v", got)
	}
	got = tmInput(t, "<write><parameter=path>f</parameter><parameter=content>[1,2]</parameter></write>")
	if got["content"] != "[1,2]" {
		t.Fatalf("tool-tag hermes params must stay strings too: %#v", got["content"])
	}
}

func TestTextMode_DSMLStringHint(t *testing.T) {
	wrap := func(attr string) string {
		return "<｜｜DSML｜｜invoke name=\"write\"><｜｜DSML｜｜parameter name=\"path\" string=\"true\">a.json</｜｜DSML｜｜parameter>" +
			"<｜｜DSML｜｜parameter name=\"content\"" + attr + ">[1,2,3]</｜｜DSML｜｜parameter></｜｜DSML｜｜invoke>"
	}
	if got := tmInput(t, wrap(` string="true"`)); got["content"] != "[1,2,3]" {
		t.Fatalf("string=true must stay text: %#v", got["content"])
	}
	if got := tmInput(t, wrap(` string="false"`)); !reflect.DeepEqual(got["content"], []any{1.0, 2.0, 3.0}) {
		t.Fatalf("string=false must decode: %#v", got["content"])
	}
	if got := tmInput(t, wrap("")); got["content"] != "[1,2,3]" {
		t.Fatalf("no hint + string schema must stay text: %#v", got["content"])
	}
}
