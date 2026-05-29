package llm

import "testing"

// param-name-as-tag shape some local models emit instead of JSON args:
// each arg wrapped in its own `<KEY>VALUE</KEY>` block.
func TestParseToolArgs_ParamNameAsTag(t *testing.T) {
	body := "<path>\n/tmp/prefix.go\n</path>\n<old>\n\treturn \"hello\"\n</old>\n<new>\n\treturn \"hi\"\n</new>\n</edit"
	args := parseToolArgs(body)
	if _, bad := args["_parse_error"]; bad {
		t.Fatalf("param-tag shape should parse, got error: %v", args["_parse_error"])
	}
	want := map[string]string{
		"path": "/tmp/prefix.go",
		"old":  "\treturn \"hello\"",
		"new":  "\treturn \"hi\"",
	}
	for k, v := range want {
		if got, _ := args[k].(string); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

// guard: a real JSON write payload whose content contains markup must NOT be
// hijacked by the fallback — strict JSON parse wins first.
func TestParseToolArgs_JSONWithMarkupNotHijacked(t *testing.T) {
	body := `{"path":"x.html","content":"<old>keep this</old>"}`
	args := parseToolArgs(body)
	if c, _ := args["content"].(string); c != "<old>keep this</old>" {
		t.Fatalf("content = %q, want markup preserved", c)
	}
}

// guard: mixed prose + a stray tag is not the param-tag shape; must stay an
// error rather than be silently misparsed.
func TestParseToolArgs_NotParamTagStaysError(t *testing.T) {
	body := "here you go <path>x</path> and some trailing prose"
	args := parseToolArgs(body)
	if _, bad := args["_parse_error"]; !bad {
		t.Fatalf("non-pure-tag body should stay a parse error, got %v", args)
	}
}
