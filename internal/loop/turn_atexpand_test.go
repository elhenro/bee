package loop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// the engine expands @path for the model but must leave the caller's content
// untouched: the TUI keeps the same slice in its scrollback.
func TestRunWithContent_AtExpandLeavesCallerContentAlone(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("FILE-BODY"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &captureProvider{}
	eng, _ := newEngine(p, tools.NewRegistry())
	eng.Cwd = dir
	content := []types.ContentBlock{{Type: types.BlockText, Text: "explain @notes.txt"}}
	if _, err := eng.RunWithContent(context.Background(), content); err != nil {
		t.Fatal(err)
	}
	if content[0].Text != "explain @notes.txt" {
		t.Fatalf("caller content mutated: %q", content[0].Text)
	}
	sent := p.reqs[0].Messages[0].Content[0].Text
	if !strings.Contains(sent, "FILE-BODY") {
		t.Fatalf("model did not get the expanded file: %q", sent)
	}
}
