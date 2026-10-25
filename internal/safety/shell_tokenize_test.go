package safety

import (
	"strings"
	"testing"
)

// tok is a compact expectation for a decoded token.
type tok struct {
	kind TokenKind
	val  string
}

func word(v string) tok { return tok{TokenWord, v} }
func op(v string) tok   { return tok{TokenOp, v} }

func TestTokenize_Table(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		want    []tok
		wantErr bool
	}{
		{"empty", "", nil, false},
		{"whitespace only", "   \t \n ", nil, false},
		{"simple words", "ls -la", []tok{word("ls"), word("-la")}, false},
		{"multiple spaces", "echo   hi", []tok{word("echo"), word("hi")}, false},
		{"operators distinct", "a && b || c ; d | e", []tok{
			word("a"), op("&&"), word("b"), op("||"), word("c"), op(";"), word("d"), op("|"), word("e"),
		}, false},
		{"operator attached", "echo hi;ls", []tok{word("echo"), word("hi"), op(";"), word("ls")}, false},
		{"single quoted", "echo 'hello world'", []tok{word("echo"), word("hello world")}, false},
		{"double quoted", `echo "a b"`, []tok{word("echo"), word("a b")}, false},
		{"adjacent quoting", `echo "a"'b'c`, []tok{word("echo"), word("abc")}, false},
		{"escaped space", `echo a\ b`, []tok{word("echo"), word("a b")}, false},
		{"escaped dollar", `echo \$HOME`, []tok{word("echo"), word("$HOME")}, false},
		{"empty quotes", `echo ""`, []tok{word("echo"), word("")}, false},
		{"redirect in", "cat < in", []tok{word("cat"), op("<"), word("in")}, false},
		{"redirect out", "cat > out", []tok{word("cat"), op(">"), word("out")}, false},
		{"redirect append", "echo hi >> log", []tok{word("echo"), word("hi"), op(">>"), word("log")}, false},
		{"heredoc op", "cat << EOF", []tok{word("cat"), op("<<"), word("EOF")}, false},
		{"fd redirect", "cmd 2> err", []tok{word("cmd"), op("2>"), word("err")}, false},
		{"number stays word", "echo 42", []tok{word("echo"), word("42")}, false},
		{"dollar preserved in dq", `echo "$HOME"`, []tok{word("echo"), word("$HOME")}, false},
		{"dq escape quote", `echo "a\"b"`, []tok{word("echo"), word(`a"b`)}, false},
		{"dq escape backslash", `echo "a\\b"`, []tok{word("echo"), word(`a\b`)}, false},
		{"dq holds single", `echo "it's fine"`, []tok{word("echo"), word("it's fine")}, false},
		{"sq holds double", `echo 'say "hi"'`, []tok{word("echo"), word(`say "hi"`)}, false},
		{"line continuation", "echo a\\\nb", []tok{word("echo"), word("ab")}, false},
		{"amp background", "sleep 1 & echo x", []tok{word("sleep"), word("1"), op("&"), word("echo"), word("x")}, false},
		{"unterminated single", `echo 'abc`, nil, true},
		{"unterminated double", `echo "abc`, nil, true},
		{"unterminated after adj", `echo "a"'b`, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Tokenize(tc.src)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Tokenize(%q) = %v; want error", tc.src, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Tokenize(%q) unexpected error: %v", tc.src, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("Tokenize(%q) = %d tokens %v; want %d %v", tc.src, len(got), got, len(tc.want), tc.want)
			}
			for i, w := range tc.want {
				if got[i].Kind != w.kind || got[i].Value != w.val {
					t.Fatalf("token %d of %q = {%d %q}; want {%d %q}", i, tc.src, got[i].Kind, got[i].Value, w.kind, w.val)
				}
			}
		})
	}
}

func TestTokenize_SegmentQuoteContext(t *testing.T) {
	got, err := Tokenize(`cmd "a"'b'c $x`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 tokens, got %d: %v", len(got), got)
	}
	w := got[1]
	if w.Value != "abc" {
		t.Fatalf("word value = %q; want abc", w.Value)
	}
	want := []Segment{
		{Text: "a", Quote: QuoteDouble},
		{Text: "b", Quote: QuoteSingle},
		{Text: "c", Quote: QuoteNone},
	}
	if len(w.Segments) != len(want) {
		t.Fatalf("segments = %v; want %v", w.Segments, want)
	}
	for i := range want {
		if w.Segments[i] != want[i] {
			t.Fatalf("segment %d = %+v; want %+v", i, w.Segments[i], want[i])
		}
	}
	if got[2].Value != "$x" {
		t.Fatalf("third token = %q; want $x", got[2].Value)
	}
	if got[2].Segments[0].Quote != QuoteNone {
		t.Fatalf("unquoted word segment kind = %v; want QuoteNone", got[2].Segments[0].Quote)
	}
}

func TestTokenize_RawPreserved(t *testing.T) {
	got, err := Tokenize(`echo "a b"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got[1].Raw != `"a b"` {
		t.Fatalf("Raw = %q; want %q", got[1].Raw, `"a b"`)
	}
}

func TestTokenize_UnterminatedQuoteMentionsPosition(t *testing.T) {
	_, err := Tokenize(`echo "abc`)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "unterminated double quote") {
		t.Fatalf("error = %v; want unterminated double quote", err)
	}
}

func TestTokenize_EscapedMultibyteKeepsBytes(t *testing.T) {
	got, err := Tokenize(`a\é b`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got[0].Value != "aé" || got[0].Segments[0].Text != "aé" {
		t.Fatalf("value %q, segment %q; want aé for both", got[0].Value, got[0].Segments[0].Text)
	}
}
