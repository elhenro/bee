package grep

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func runGrep(t *testing.T, content string, in map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := New(dir).Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return res.Content
}

func TestScanWithContext_WindowsAndGaps(t *testing.T) {
	// lines: 1 a, 2 b, 3 HIT, 4 c, 5 d, 6 e, 7 f, 8 HIT, 9 g
	src := "a\nb\nHIT\nc\nd\ne\nf\nHIT\ng\n"
	got := runGrep(t, src, map[string]any{"pattern": "HIT", "context": 2})
	want := strings.Join([]string{
		"f.txt:1-a", "f.txt:2-b", "f.txt:3:HIT", "f.txt:4-c", "f.txt:5-d",
		"f.txt:6-e", "f.txt:7-f", "f.txt:8:HIT", "f.txt:9-g",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestScanWithContext_AfterContextDoesNotLeakIntoBefore(t *testing.T) {
	// line 3 is after-context of the first hit, so it must not repeat as
	// before-context of the second.
	got := runGrep(t, "HIT\nx\ny\nHIT\n", map[string]any{"pattern": "HIT", "context": 3})
	if strings.Count(got, "f.txt:2-x") != 1 || strings.Count(got, "f.txt:3-y") != 1 {
		t.Errorf("duplicated or missing context:\n%s", got)
	}
}

func TestScanWithContext_Budget(t *testing.T) {
	var b strings.Builder
	for range 50 {
		b.WriteString("HIT\n")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &Tool{root: dir, max: 7}
	res, _ := g.Run(context.Background(), map[string]any{"pattern": "HIT", "context": 1})
	if n := len(strings.Split(res.Content, "\n")); n != 7 {
		t.Errorf("want 7 lines, got %d:\n%s", n, res.Content)
	}
}

func TestGrep_LongLineClipped(t *testing.T) {
	long := "HIT" + strings.Repeat("é", 2000)
	for _, ctx := range []int{0, 1} {
		got := runGrep(t, "a\n"+long+"\nb\n", map[string]any{"pattern": "HIT", "context": ctx})
		if len(got) > 2*maxLineWidth+100 {
			t.Errorf("context=%d: line not clipped, %d bytes", ctx, len(got))
		}
		if !strings.Contains(got, "… [+") {
			t.Errorf("context=%d: missing clip marker:\n%.200s", ctx, got)
		}
		if !utf8.ValidString(got) {
			t.Errorf("context=%d: clip split a rune", ctx)
		}
	}
}

func TestClipLine_ShortUntouched(t *testing.T) {
	s := strings.Repeat("x", maxLineWidth)
	if clipLine([]byte(s)) != s {
		t.Error("line at the cap was altered")
	}
}
