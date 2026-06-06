package mux

import (
	"reflect"
	"testing"
)

func TestInTmux(t *testing.T) {
	t.Setenv("TMUX", "")
	if InTmux() {
		t.Fatal("InTmux: empty TMUX should be false")
	}
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1234,0")
	if !InTmux() {
		t.Fatal("InTmux: set TMUX should be true")
	}
}

func TestResolveEditor(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if got := ResolveEditor(""); got != "vim" {
		t.Fatalf("fallback: got %q want vim", got)
	}
	t.Setenv("EDITOR", "nano")
	if got := ResolveEditor(""); got != "nano" {
		t.Fatalf("EDITOR: got %q want nano", got)
	}
	t.Setenv("VISUAL", "code -w")
	if got := ResolveEditor(""); got != "code -w" {
		t.Fatalf("VISUAL beats EDITOR: got %q", got)
	}
	if got := ResolveEditor("hx"); got != "hx" {
		t.Fatalf("explicit cfg beats env: got %q want hx", got)
	}
}

func TestEditorCommand(t *testing.T) {
	cases := []struct {
		editor, file string
		line         int
		want         string
	}{
		{"vim", "main.go", 12, "vim +12 main.go"},
		{"vim", "main.go", 0, "vim main.go"},
		{"nvim", "", 0, "nvim ."},
		{"vim", "my file.go", 0, "vim 'my file.go'"},
	}
	for _, c := range cases {
		if got := EditorCommand(c.editor, c.file, c.line); got != c.want {
			t.Errorf("EditorCommand(%q,%q,%d)=%q want %q", c.editor, c.file, c.line, got, c.want)
		}
	}
}

func TestWindowArgsNew(t *testing.T) {
	got := windowArgs("vim", "/work", "vim main.go", []string{"bee", "shell"})
	want := []string{"new-window", "-n", "vim", "-c", "/work", "vim main.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("new window:\n got %v\nwant %v", got, want)
	}
}

func TestWindowArgsReuse(t *testing.T) {
	got := windowArgs("vim", "/work", "vim main.go", []string{"bee", "vim"})
	want := []string{"select-window", "-t", "vim"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reuse window:\n got %v\nwant %v", got, want)
	}
}
