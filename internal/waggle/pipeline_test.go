package waggle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/tools/skillexec"
)

// record -> mine -> render -> parse -> run, end to end with no model. A repeated
// ls+read route crystallizes into a waggle whose script reproduces the output.
func TestPipeline_RecordMinePromoteRun(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := NewRecorder(50)
	route := []Call{
		{Tool: "ls", Args: map[string]string{"path": dir}},
		{Tool: "read", Args: map[string]string{"path": filepath.Join(dir, "a.txt")}},
	}
	for i := 0; i < 2; i++ {
		rec.Record(cloneCall(route[0]))
		rec.Record(cloneCall(route[1]))
	}
	cands := Mine(rec.Calls(), MineConfig{})
	if len(cands) == 0 {
		t.Fatal("no candidate mined")
	}
	md, ok := Render("probe", cands[0], ScopeProject)
	if !ok {
		t.Fatal("render failed")
	}
	s, err := skills.Parse("probe.md", []byte(md))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, md)
	}
	tool, err := skillexec.New(s)
	if err != nil {
		t.Fatalf("skillexec: %v", err)
	}
	res, err := tool.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || !strings.Contains(res.Content, "a.txt") || !strings.Contains(res.Content, "alpha") {
		t.Errorf("pipeline output wrong: err=%v %q", res.IsError, res.Content)
	}
}
