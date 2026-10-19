package bgreg

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// resume restates Task as the original assignment, so the sidecar keeps it whole
func TestWriteKeepsFullTask(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	task := strings.Repeat("Implement the feature with care. ", 30) + "TAILMARK"
	if err := Write(Status{SessionID: "long-task", Task: task, LastResponse: task}); err != nil {
		t.Fatal(err)
	}
	got, err := Read("long-task")
	if err != nil {
		t.Fatal(err)
	}
	if got.Task != task {
		t.Fatalf("task cut to %d bytes, want %d", len(got.Task), len(task))
	}
	if len(got.LastResponse) > previewMax+len("…") {
		t.Fatalf("last response not truncated: %d bytes", len(got.LastResponse))
	}
}

func TestTruncateKeepsRunesWhole(t *testing.T) {
	for _, pad := range []string{"", "a", "ab"} {
		got := Truncate(pad + strings.Repeat("ü", 300))
		if !utf8.ValidString(got) {
			t.Fatalf("pad %q: invalid utf-8 after truncate", pad)
		}
		if !strings.HasSuffix(got, "…") || len(got) > previewMax+len("…") {
			t.Fatalf("pad %q: bad truncation, %d bytes", pad, len(got))
		}
	}
}
