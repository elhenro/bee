package arena

import (
	"os"
	"strings"
	"testing"
)

func TestWritePostMortemWritesReadableRecord(t *testing.T) {
	dir := t.TempDir()
	r := MatchResult{
		MatchID: "abc-123", Winner: "red", Reason: "exfiltration",
		RedModel: "claude-opus-4-8", BlueModel: "local-8b",
		RedBalance: 90000, BlueBalance: 0,
	}
	path, err := WritePostMortem(dir, r)
	if err != nil {
		t.Fatalf("WritePostMortem: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	body := string(b)
	for _, want := range []string{"red", "exfiltration", "claude-opus-4-8"} {
		if !strings.Contains(body, want) {
			t.Fatalf("post-mortem missing %q:\n%s", want, body)
		}
	}
}
