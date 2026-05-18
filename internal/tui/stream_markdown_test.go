package tui

import (
	"strings"
	"testing"
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
