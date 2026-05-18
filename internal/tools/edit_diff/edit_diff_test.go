package edit_diff

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestEditDiff_NthOccurrence(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("foo bar foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New(dir)
	res, err := e.Run(context.Background(), map[string]any{
		"path":       p,
		"old":        "foo",
		"new":        "qux",
		"occurrence": 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "foo bar qux" {
		t.Errorf("want 'foo bar qux', got %q", string(data))
	}
}

func TestEditDiff_DefaultFirst(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("foo bar foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New(dir)
	res, _ := e.Run(context.Background(), map[string]any{
		"path": p,
		"old":  "foo",
		"new":  "qux",
	})
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "qux bar foo" {
		t.Errorf("want 'qux bar foo', got %q", string(data))
	}
}

func TestEditDiff_NotFound(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New(dir)
	res, _ := e.Run(context.Background(), map[string]any{
		"path": p, "old": "zzz", "new": "qqq",
	})
	if !res.IsError {
		t.Errorf("want IsError when old missing")
	}
}

func TestEditDiff_OccurrenceTooHigh(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New(dir)
	res, _ := e.Run(context.Background(), map[string]any{
		"path": p, "old": "foo", "new": "qux", "occurrence": 2,
	})
	if !res.IsError {
		t.Errorf("want IsError when occurrence > count")
	}
}

func TestEditDiff_MissingPath(t *testing.T) {
	e := New(t.TempDir())
	res, _ := e.Run(context.Background(), map[string]any{"old": "a", "new": "b"})
	if !res.IsError {
		t.Errorf("want IsError when path missing")
	}
}

func TestEditDiff_MissingOld(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(p, []byte("x"), 0o644)
	e := New(dir)
	res, _ := e.Run(context.Background(), map[string]any{"path": p, "new": "y"})
	if !res.IsError {
		t.Errorf("want IsError when old missing")
	}
}

func TestEditDiff_RejectsEmptyNew(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("foo bar foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := New(dir).Run(context.Background(), map[string]any{
		"path": p, "old": "foo", "new": "", "replace_all": true,
	})
	if !res.IsError {
		t.Fatalf("want IsError when new is empty")
	}
	if !strings.Contains(res.Content, "non-empty") {
		t.Errorf("want 'non-empty' in msg, got: %s", res.Content)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "foo bar foo" {
		t.Errorf("file must be untouched on empty-new rejection, got %q", data)
	}
}

func TestEditDiff_ReplaceAll(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("foo bar foo baz foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := New(dir).Run(context.Background(), map[string]any{
		"path":        p,
		"old":         "foo",
		"new":         "qux",
		"replace_all": true,
	})
	if err != nil || res.IsError {
		t.Fatalf("err: %v %s", err, res.Content)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "qux bar qux baz qux" {
		t.Errorf("want all-replaced, got %q", data)
	}
	if !strings.Contains(res.Content, "replaced 3 occurrence(s)") {
		t.Errorf("summary missing count: %s", res.Content)
	}
}

func TestEditDiff_CountGuardPasses(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("foo foo foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := New(dir).Run(context.Background(), map[string]any{
		"path": p, "old": "foo", "new": "qux",
		"count": 3, "replace_all": true,
	})
	if res.IsError {
		t.Fatalf("count guard should pass when actual==expected: %s", res.Content)
	}
}

func TestEditDiff_CountGuardFails(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("foo foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := New(dir).Run(context.Background(), map[string]any{
		"path": p, "old": "foo", "new": "qux", "count": 3,
	})
	if !res.IsError {
		t.Fatalf("want IsError when count mismatches")
	}
	if !strings.Contains(res.Content, "count guard") {
		t.Errorf("want 'count guard' in msg, got: %s", res.Content)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "foo foo" {
		t.Errorf("file mutated under count-guard rejection: %q", data)
	}
}

func TestEditDiff_EchoRendersContext(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("a\nb\nfoo\nd\ne\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := New(dir).Run(context.Background(), map[string]any{"path": p, "old": "foo", "new": "qux"})
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	// echo should render 2-line context window with hashline anchors
	if !strings.Contains(res.Content, "region 1") {
		t.Fatalf("missing region header in echo:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, " │ qux") {
		t.Fatalf("missing replaced line in echo:\n%s", res.Content)
	}
}

func TestEditDiff_StringifiedOccurrence(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("foo bar foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := New(dir).Run(context.Background(), map[string]any{
		"path": p, "old": "foo", "new": "qux", "occurrence": "2",
	})
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "foo bar qux" {
		t.Errorf("stringified occurrence not honored: %q", data)
	}
}

func TestEditDiff_FilterNilAllowsAll(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	if err := os.WriteFile(p, []byte("foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewWithFilter(dir, nil)
	res, _ := e.Run(context.Background(), map[string]any{"path": p, "old": "foo", "new": "bar"})
	if res.IsError {
		t.Fatalf("nil filter must allow: %s", res.Content)
	}
}

func TestEditDiff_FilterAllowsMatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "foo.md")
	if err := os.WriteFile(p, []byte("foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewWithFilter(dir, regexp.MustCompile(`\.md$`))
	res, _ := e.Run(context.Background(), map[string]any{"path": p, "old": "foo", "new": "bar"})
	if res.IsError {
		t.Fatalf("md path must pass: %s", res.Content)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "bar" {
		t.Errorf("want 'bar', got %q", data)
	}
}

func TestEditDiff_FilterRejectsMiss(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "foo.go")
	if err := os.WriteFile(p, []byte("foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewWithFilter(dir, regexp.MustCompile(`\.md$`))
	res, _ := e.Run(context.Background(), map[string]any{"path": p, "old": "foo", "new": "bar"})
	if !res.IsError {
		t.Errorf("want IsError for non-md path")
	}
	if !strings.Contains(res.Content, "denied") {
		t.Errorf("want 'denied' in msg, got: %s", res.Content)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "foo" {
		t.Errorf("file must be untouched on rejection, got %q", data)
	}
}
