package read

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadTextFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := New().Run(context.Background(), map[string]any{"path": p})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	for _, want := range []string{"1 │ alpha", "2 │ beta", "3 │ gamma"} {
		if !strings.Contains(res.Content, want) {
			t.Fatalf("missing %q in:\n%s", want, res.Content)
		}
	}
}

func TestReadOffsetLimit(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("a\nb\nc\nd\ne\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := New().Run(context.Background(), map[string]any{
		"path":   p,
		"offset": 2,
		"limit":  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	if !strings.Contains(res.Content, "2 │ b") || !strings.Contains(res.Content, "3 │ c") {
		t.Fatalf("offset/limit wrong:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "1 │ a") || strings.Contains(res.Content, "4 │ d") {
		t.Fatalf("offset/limit not honored:\n%s", res.Content)
	}
}

func TestListDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := New().Run(context.Background(), map[string]any{"path": dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	if !strings.Contains(res.Content, "a.txt") {
		t.Fatalf("missing a.txt:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "sub/") {
		t.Fatalf("missing dir marker for sub/:\n%s", res.Content)
	}
}

func TestMissingFile(t *testing.T) {
	res, _ := New().Run(context.Background(), map[string]any{"path": "/nope/nothing/here"})
	if !res.IsError {
		t.Fatal("expected error for missing path")
	}
}

func TestBinaryFileRefused(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bin")
	// NUL byte trips binary detection
	if err := os.WriteFile(p, []byte{0x7f, 'E', 'L', 'F', 0, 1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := New().Run(context.Background(), map[string]any{"path": p})
	if !res.IsError {
		t.Fatal("expected refusal for binary file")
	}
	if !strings.Contains(strings.ToLower(res.Content), "binary") {
		t.Fatalf("error msg should mention binary: %s", res.Content)
	}
}

func TestEmptyPath(t *testing.T) {
	res, _ := New().Run(context.Background(), map[string]any{"path": ""})
	if !res.IsError {
		t.Fatal("expected error for empty path")
	}
}

func TestOffsetBeyondEOF(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := New().Run(context.Background(), map[string]any{"path": p, "offset": 100})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	if !strings.Contains(res.Content, "no lines") {
		t.Fatalf("expected no-lines message:\n%s", res.Content)
	}
}

func TestCacheHitReturnsStub(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := New()
	first, err := tool.Run(context.Background(), map[string]any{"path": p})
	if err != nil || first.IsError {
		t.Fatalf("first read failed: %v %s", err, first.Content)
	}
	if !strings.Contains(first.Content, "alpha") {
		t.Fatalf("first read missing body: %s", first.Content)
	}
	second, err := tool.Run(context.Background(), map[string]any{"path": p})
	if err != nil || second.IsError {
		t.Fatalf("second read failed: %v %s", err, second.Content)
	}
	if !strings.Contains(second.Content, "cache") {
		t.Fatalf("cache hit expected, got body: %s", second.Content)
	}
	if strings.Contains(second.Content, "alpha") {
		t.Fatalf("cache hit should drop body, got: %s", second.Content)
	}
}

func TestCacheInvalidatedOnEdit(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := New()
	if _, err := tool.Run(context.Background(), map[string]any{"path": p}); err != nil {
		t.Fatal(err)
	}
	// rewrite content — mtime+size change invalidates cache entry
	if err := os.WriteFile(p, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := tool.Run(context.Background(), map[string]any{"path": p})
	if err != nil || res.IsError {
		t.Fatalf("post-edit read failed: %v %s", err, res.Content)
	}
	if !strings.Contains(res.Content, "gamma") {
		t.Fatalf("post-edit read should return new body: %s", res.Content)
	}
	if strings.Contains(res.Content, "cache") {
		t.Fatalf("post-edit read should not be cached: %s", res.Content)
	}
}

func TestReadTail(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&b, "line%03d\n", i)
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := New().Run(context.Background(), map[string]any{"path": p, "tail": 10})
	if err != nil || res.IsError {
		t.Fatalf("err: %v %s", err, res.Content)
	}
	// expect last 10 lines (091..100) with their true line numbers
	for _, want := range []string{"91 │ line091", "100 │ line100"} {
		if !strings.Contains(res.Content, want) {
			t.Fatalf("missing %q in:\n%s", want, res.Content)
		}
	}
	if strings.Contains(res.Content, "line001") {
		t.Fatalf("tail leaked head lines:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "(showing last 10 of 100 lines") {
		t.Fatalf("missing tail footer:\n%s", res.Content)
	}
}

func TestReadStringifiedInts(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("a\nb\nc\nd\ne\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// providers sometimes serialize ints as strings — must still parse
	res, err := New().Run(context.Background(), map[string]any{
		"path":   p,
		"offset": "2",
		"limit":  "2",
	})
	if err != nil || res.IsError {
		t.Fatalf("err: %v %s", err, res.Content)
	}
	if !strings.Contains(res.Content, "2 │ b") || !strings.Contains(res.Content, "3 │ c") {
		t.Fatalf("stringified ints not honored:\n%s", res.Content)
	}
}

func TestSpec(t *testing.T) {
	s := New().Spec()
	if s.Name != "read" {
		t.Fatalf("wrong name: %s", s.Name)
	}
	if s.Schema == nil {
		t.Fatal("nil schema")
	}
}

// hashlineAlphabet mirrors apply_patch.hashAlphabet — kept local so the test
// asserts the contract without exposing the constant.
const hashlineAlphabet = "ZPMQVRWSNKTXJBYH"

func TestRun_HashlineEmitsTags(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := New().Run(context.Background(), map[string]any{
		"path":     p,
		"hashline": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	lines := strings.Split(res.Content, "\n")
	// drop the footer `(end of file; N lines total)` for the per-line checks.
	if len(lines) > 0 && strings.HasPrefix(lines[len(lines)-1], "(") {
		lines = lines[:len(lines)-1]
	}
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d:\n%s", len(lines), res.Content)
	}
	for i, l := range lines {
		hash := strings.IndexByte(l, '#')
		sep := strings.Index(l, " │ ")
		if hash < 0 || sep < 0 || sep-hash != 4 {
			t.Fatalf("line %d missing #XYZ before separator: %q", i+1, l)
		}
		tag := l[hash+1 : sep]
		if len(tag) != 3 {
			t.Fatalf("line %d tag wrong length: %q", i+1, tag)
		}
		for j, r := range tag {
			if !strings.ContainsRune(hashlineAlphabet, r) {
				t.Fatalf("line %d tag char %d (%q) not in alphabet %q", i+1, j, r, hashlineAlphabet)
			}
		}
	}
}

func TestRun_HashlineDefaultOff(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := New().Run(context.Background(), map[string]any{"path": p})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	// no '#' should appear between line-number prefix and the separator.
	// the trailing `(N lines total)` footer is metadata, skip it.
	for _, l := range strings.Split(res.Content, "\n") {
		if strings.HasPrefix(l, "(") {
			continue
		}
		sep := strings.Index(l, " │ ")
		if sep < 0 {
			t.Fatalf("missing separator in line: %q", l)
		}
		if strings.ContainsRune(l[:sep], '#') {
			t.Fatalf("hashline anchor leaked when disabled: %q", l)
		}
	}
}

func TestRead_SymlinkToSensitiveBlocked(t *testing.T) {
	dir := t.TempDir()
	// target basename matches the secret-file pattern (id_rsa)
	target := filepath.Join(dir, "id_rsa")
	if err := os.WriteFile(target, []byte("PRIVATE KEY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "innocent.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	res, _ := New().Run(context.Background(), map[string]any{"path": link})
	if !res.IsError {
		t.Fatalf("symlink to sensitive target should be refused, got: %s", res.Content)
	}
	if !strings.Contains(res.Content, "refused") {
		t.Fatalf("expected refusal message, got: %s", res.Content)
	}
}

func TestRun_HashlineIgnoredForDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := New().Run(context.Background(), map[string]any{
		"path":     dir,
		"hashline": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	// directory listings emit entries one per line — no '#' should appear
	for _, l := range strings.Split(res.Content, "\n") {
		if strings.ContainsRune(l, '#') {
			t.Fatalf("hashline tag leaked into dir listing: %q", l)
		}
	}
	if !strings.Contains(res.Content, "a.txt") || !strings.Contains(res.Content, "sub/") {
		t.Fatalf("directory listing changed shape:\n%s", res.Content)
	}
}

func TestSpec_LimitDescriptionShowsActualBounds(t *testing.T) {
	// tiny profile limits — description must reflect them so the model
	// knows it can pass `limit: 500` rather than chunked-reading.
	tool := NewWithLimits(100, 500)
	props, _ := tool.Spec().Schema["properties"].(map[string]any)
	limitProp, _ := props["limit"].(map[string]any)
	desc, _ := limitProp["description"].(string)
	if !strings.Contains(desc, "Default 100") {
		t.Errorf("expected default 100 in limit desc, got: %q", desc)
	}
	if !strings.Contains(desc, "up to 500") {
		t.Errorf("expected max 500 in limit desc, got: %q", desc)
	}
}

func TestSpec_LimitDescriptionPackageDefaults(t *testing.T) {
	// non-tiny: package defaults (2000 / 10000) should render.
	tool := New()
	props, _ := tool.Spec().Schema["properties"].(map[string]any)
	limitProp, _ := props["limit"].(map[string]any)
	desc, _ := limitProp["description"].(string)
	if !strings.Contains(desc, "Default 2000") {
		t.Errorf("expected default 2000 in limit desc, got: %q", desc)
	}
	if !strings.Contains(desc, "up to 10000") {
		t.Errorf("expected max 10000 in limit desc, got: %q", desc)
	}
}
