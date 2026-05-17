package knowledge_write

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/tools"
)

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
	tags, ok := props["tags"].(map[string]any)
	if !ok {
		t.Fatal("missing tags property")
	}
	maxItems, ok := tags["maxItems"]
	if !ok || maxItems != 5 {
		t.Fatalf("expected maxItems=5 on tags, got %v", maxItems)
	}
}

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
	if _, err := os.Stat(filepath.Join(dir, "test-record.md")); err != nil {
		t.Fatalf("record file not written: %v", err)
	}
}

func TestWriteOverwrite(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)

	res, err := tool.Run(context.Background(), validInput())
	if err != nil || res.IsError {
		t.Fatalf("first write failed: %v %s", err, res.Content)
	}

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
	if !strings.Contains(string(b), "expires: 2025-09-01T00:00:00Z") {
		t.Fatalf("expected RFC 3339 expires in file,\ngot:\n%s", string(b))
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
	if !strings.Contains(string(b), "expires: 2025-09-01T00:00:00Z") {
		t.Fatalf("expected RFC 3339 expires in file,\ngot:\n%s", string(b))
	}
}

func TestWriteExpiresInvalid(t *testing.T) {
	dir := toolDir(t)
	tool := New(dir)
	in := validInput()
	in["expires_at"] = "next tuesday"
	res, _ := tool.Run(context.Background(), in)
	if !res.IsError {
		t.Fatal("expected error for unparseable expires_at")
	}
	if !strings.Contains(res.Content, "expires_at: cannot parse") {
		t.Fatalf("expected parse error, got: %q", res.Content)
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
	content := string(b)
	if !strings.Contains(content, "line one") {
		t.Fatalf("expected body content, got:\n%s", content)
	}
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
	got, err := parseExpires(nil)
	if err != nil || !got.IsZero() {
		t.Fatalf("nil: got=(%v, %v), want zero+noerr", got, err)
	}
	got, err = parseExpires("")
	if err != nil || !got.IsZero() {
		t.Fatalf("empty string: got=(%v, %v), want zero+noerr", got, err)
	}
}

func TestParseExpiresDate(t *testing.T) {
	got, err := parseExpires("2025-09-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Year() != 2025 || got.Month() != 9 || got.Day() != 1 {
		t.Fatalf("unexpected date: %v", got)
	}
}

func TestParseExpiresRFC3339(t *testing.T) {
	got, err := parseExpires("2025-09-01T00:00:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Year() != 2025 || got.Month() != 9 || got.Day() != 1 {
		t.Fatalf("unexpected date: %v", got)
	}
}

func TestParseExpiresUnparseable(t *testing.T) {
	_, err := parseExpires("next tuesday")
	if err == nil {
		t.Fatal("expected error for unparseable date")
	}
	if !strings.Contains(err.Error(), "cannot parse") {
		t.Fatalf("unexpected error: %v", err)
	}
}
