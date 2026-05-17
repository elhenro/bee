package knowledge_write

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

// stubProvider satisfies llm.Provider for tests that need one.
type stubProvider struct {
	resp string
}

func (s stubProvider) Name() string { return "stub" }
func (s stubProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: llm.EventTextDelta, Delta: s.resp}
	ch <- llm.Event{Type: llm.EventDone}
	close(ch)
	return ch, nil
}

func toolDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func validInput() map[string]any {
	return map[string]any{
		"name":        "test-record",
		"description": "a test record",
		"body":        "this is the body content",
	}
}

func TestSpec(t *testing.T) {
	tool := New("")
	spec := tool.Spec()
	if spec.Name != "knowledge_write" {
		t.Fatalf("name %q, want knowledge_write", spec.Name)
	}
}

func TestSpecSchema(t *testing.T) {
	tool := New("")
	spec := tool.Spec()
	props, ok := spec.Schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("missing properties map")
	}
	for _, k := range []string{"name", "description", "body"} {
		if _, ok := props[k]; !ok {
			t.Fatalf("missing required property %q", k)
		}
	}
}

// --- write tests ---

func TestWriteNew(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	res, err := tool.Run(context.Background(), validInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "stored") {
		t.Fatalf("expected 'stored' in result, got: %q", res.Content)
	}
	// verify file on disk
	if _, err := os.Stat(filepath.Join(dir, "test-record.md")); err != nil {
		t.Fatalf("record file not written: %v", err)
	}
}

func TestWriteOverwrite(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)

	// first write
	res, err := tool.Run(context.Background(), validInput())
	if err != nil || res.IsError {
		t.Fatalf("first write failed: %v %s", err, res.Content)
	}

	// second write same name, different body
	in2 := validInput()
	in2["body"] = "updated body"
	res, err = tool.Run(context.Background(), in2)
	if err != nil {
		t.Fatalf("overwrite error: %v", err)
	}
	if res.IsError {
		t.Fatalf("overwrite error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "stored") {
		t.Fatalf("expected 'stored' in result, got: %q", res.Content)
	}
	// read back and verify content
	b, err := os.ReadFile(filepath.Join(dir, "test-record.md"))
	if err != nil {
		t.Fatalf("read back failed: %v", err)
	}
	if !strings.Contains(string(b), "updated body") {
		t.Fatalf("expected updated body, got:\n%s", string(b))
	}
}

func TestWriteMissingName(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	in := validInput()
	delete(in, "name")
	res, _ := tool.Run(context.Background(), in)
	if !res.IsError {
		t.Fatal("expected error for missing name")
	}
}

func TestWriteEmptyName(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	in := validInput()
	in["name"] = ""
	res, _ := tool.Run(context.Background(), in)
	if !res.IsError {
		t.Fatal("expected error for empty name")
	}
}

func TestWriteMissingBody(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	in := validInput()
	delete(in, "body")
	res, _ := tool.Run(context.Background(), in)
	if !res.IsError {
		t.Fatal("expected error for missing body")
	}
}

func TestWriteEmptyBody(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	in := validInput()
	in["body"] = ""
	res, _ := tool.Run(context.Background(), in)
	if !res.IsError {
		t.Fatal("expected error for empty body")
	}
	if !strings.Contains(res.Content, "non-empty") {
		t.Fatalf("unexpected error message: %q", res.Content)
	}
}

func TestWriteMissingDescription(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	in := validInput()
	delete(in, "description")
	res, _ := tool.Run(context.Background(), in)
	if !res.IsError {
		t.Fatal("expected error for missing description")
	}
}

func TestWriteBadSlug(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	in := validInput()
	in["name"] = "has space"
	res, _ := tool.Run(context.Background(), in)
	if !res.IsError {
		t.Fatal("expected error for bad slug")
	}
}

func TestWriteTagsValid(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	in := validInput()
	in["tags"] = []any{"deployment", "ci", "ops"}
	res, err := tool.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	// read file and confirm tags rendered
	b, err := os.ReadFile(filepath.Join(dir, "test-record.md"))
	if err != nil {
		t.Fatalf("read back failed: %v", err)
	}
	if !strings.Contains(string(b), "[deployment, ci, ops]") {
		t.Fatalf("expected tags in frontmatter, got:\n%s", string(b))
	}
}

func TestWriteTagsInvalid(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	in := validInput()
	in["tags"] = []any{"UPPERCASE"}
	res, _ := tool.Run(context.Background(), in)
	if !res.IsError {
		t.Fatal("expected error for invalid tag (uppercase)")
	}
}

func TestWriteTagsTooMany(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	in := validInput()
	in["tags"] = []any{"a", "b", "c", "d", "e", "f"}
	res, _ := tool.Run(context.Background(), in)
	if !res.IsError {
		t.Fatal("expected error for >5 tags")
	}
}

func TestWritePriorityOutOfRange(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	in := validInput()
	in["priority"] = 6
	res, _ := tool.Run(context.Background(), in)
	if !res.IsError {
		t.Fatal("expected error for priority 6")
	}
}

func TestWritePriorityZeroDefaults(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	in := validInput()
	in["priority"] = 0
	res, err := tool.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	b, err := os.ReadFile(filepath.Join(dir, "test-record.md"))
	if err != nil {
		t.Fatalf("read back failed: %v", err)
	}
	if !strings.Contains(string(b), "priority: 3") {
		t.Fatalf("expected priority 3 (default), got:\n%s", string(b))
	}
}

func TestWriteExpiresDate(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	in := validInput()
	in["expires_at"] = "2025-09-01"
	res, err := tool.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	b, err := os.ReadFile(filepath.Join(dir, "test-record.md"))
	if err != nil {
		t.Fatalf("read back failed: %v", err)
	}
	if !strings.Contains(string(b), "2025-09-01") {
		t.Fatalf("expected expires date in file, got:\n%s", string(b))
	}
}

func TestWriteExpiresRFC3339(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	in := validInput()
	in["expires_at"] = "2025-09-01T00:00:00Z"
	res, err := tool.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	b, err := os.ReadFile(filepath.Join(dir, "test-record.md"))
	if err != nil {
		t.Fatalf("read back failed: %v", err)
	}
	if !strings.Contains(string(b), "2025-09-01") {
		t.Fatalf("expected expires date in file, got:\n%s", string(b))
	}
}

func TestWriteExpiresInvalid(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	in := validInput()
	in["expires_at"] = "next tuesday"
	// unparseable expires silently ignored — tool does not error
	// (expires_at is optional, silent no-expiry is fine)
	res, err := tool.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	// file should have expires: never (not a real date)
	b, err := os.ReadFile(filepath.Join(dir, "test-record.md"))
	if err != nil {
		t.Fatalf("read back failed: %v", err)
	}
	if !strings.Contains(string(b), "expires: never") {
		t.Fatalf("expected expires: never for unparseable date, got:\n%s", string(b))
	}
}

func TestWriteBodyTrailingNewlines(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	in := validInput()
	in["body"] = "line one\n\n\n"
	res, err := tool.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	b, err := os.ReadFile(filepath.Join(dir, "test-record.md"))
	if err != nil {
		t.Fatalf("read back failed: %v", err)
	}
	// renderFile trims trailing newlines from body
	content := string(b)
	if !strings.Contains(content, "line one") {
		t.Fatalf("expected body content, got:\n%s", content)
	}
	// should NOT end with trailing blank lines
	if strings.HasSuffix(content, "\n\n") {
		t.Fatal("body should not have trailing blank lines after renderFile trim")
	}
}

func TestWritePastExpiry(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	in := validInput()
	in["expires_at"] = "2020-01-01"
	res, err := tool.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	// should write fine — staleness is a read-time concern
	if _, err := os.Stat(filepath.Join(dir, "test-record.md")); err != nil {
		t.Fatalf("record file not written: %v", err)
	}
}

func TestWriteStoreDirEmpty(t *testing.T) {
	tool := New("")
	res, _ := tool.Run(context.Background(), validInput())
	if !res.IsError {
		t.Fatal("expected error for empty store dir")
	}
}

func TestToolInterface(t *testing.T) {
	var _ tools.Tool = (*Tool)(nil)
}

// --- parseExpires unit tests ---

func TestParseExpiresEmpty(t *testing.T) {
	if !parseExpires(nil).IsZero() {
		t.Fatal("nil should return zero time")
	}
	if !parseExpires("").IsZero() {
		t.Fatal("empty string should return zero time")
	}
}

func TestParseExpiresDate(t *testing.T) {
	got := parseExpires("2025-09-01")
	if got.Year() != 2025 || got.Month() != 9 || got.Day() != 1 {
		t.Fatalf("unexpected date: %v", got)
	}
}

func TestParseExpiresRFC3339(t *testing.T) {
	got := parseExpires("2025-09-01T00:00:00Z")
	if got.Year() != 2025 || got.Month() != 9 || got.Day() != 1 {
		t.Fatalf("unexpected date: %v", got)
	}
}

func TestParseExpiresUnparseable(t *testing.T) {
	if !parseExpires("next tuesday").IsZero() {
		t.Fatal("unparseable should return zero time")
	}
}
