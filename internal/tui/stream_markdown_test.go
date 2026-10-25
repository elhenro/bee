package tui

import (
	"strings"
	"testing"
	"time"
)

func TestRewriteShellSessionFences(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "bash with $ prompt → console",
			in:   "```bash\n$ git log --oneline\nabc feat: x\ndef fix: y\n```",
			want: "```console\n$ git log --oneline\nabc feat: x\ndef fix: y\n```",
		},
		{
			name: "sh tag with $ prompt → console",
			in:   "```sh\n$ ls\nfoo\n```",
			want: "```console\n$ ls\nfoo\n```",
		},
		{
			name: "bash without prompt stays bash",
			in:   "```bash\nfor i in 1 2; do echo $i; done\n```",
			want: "```bash\nfor i in 1 2; do echo $i; done\n```",
		},
		{
			name: "non-shell fence untouched",
			in:   "```python\n$ not a prompt\n```",
			want: "```python\n$ not a prompt\n```",
		},
		{
			name: "multiple blocks rewritten independently",
			in:   "```bash\nfoo\n```\n```bash\n$ ls\n```",
			want: "```bash\nfoo\n```\n```console\n$ ls\n```",
		},
		{
			name: "leading blank lines inside block ok",
			in:   "```bash\n\n$ ls\n```",
			want: "```console\n\n$ ls\n```",
		},
		{
			name: "no fences passthrough",
			in:   "just prose with $ in it",
			want: "just prose with $ in it",
		},
		{
			name: "empty bash block untouched",
			in:   "```bash\n```",
			want: "```bash\n```",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rewriteShellSessionFences(tc.in)
			if got != tc.want {
				t.Fatalf("mismatch\nin:   %q\nwant: %q\ngot:  %q", tc.in, tc.want, got)
			}
		})
	}
}

func TestRewriteShellSessionFencesPreservesTrailingNewline(t *testing.T) {
	in := "```bash\n$ ls\n```\n"
	got := rewriteShellSessionFences(in)
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("trailing newline lost: %q", got)
	}
}

func TestTrimTrailingVisibleSpaces(t *testing.T) {
	cases := map[string]string{
		"":                                    "",
		"abc":                                 "abc",
		"   ":                                 "",
		"\x1b[31mabc\x1b[0m \x1b[0m  \x1b[0m": "\x1b[31mabc",
		"\x1b[0m  ":                           "",
		// escapes other than CSI count as visible text
		"a\x1b]0;t\x07 ": "a\x1b]0;t\x07 ",
		"a\x1b":          "a\x1b",
	}
	for in, want := range cases {
		done := make(chan string, 1)
		go func() { done <- trimTrailingVisibleSpaces(in) }()
		select {
		case got := <-done:
			if got != want {
				t.Errorf("trimTrailingVisibleSpaces(%q) = %q, want %q", in, got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("trimTrailingVisibleSpaces(%q) did not return", in)
		}
	}
}
