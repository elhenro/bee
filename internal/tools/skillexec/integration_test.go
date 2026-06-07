package skillexec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/tools"
)

// end-to-end: a real exec skill .md parsed by the registry becomes a callable
// tool that runs. Exercises parse -> RegisterExecSkills -> Run as one path.
func TestEndToEnd_ExecSkillFromDisk(t *testing.T) {
	dir := t.TempDir()
	md := "---\nname: say_hi\ntype: exec\ndescription: greet\nexec: [\"bash\", \"-c\", \"echo hi-from-disk\"]\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "say_hi.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := skills.NewRegistry()
	if err := reg.Load(dir); err != nil {
		t.Fatalf("load: %v", err)
	}
	toolReg := tools.NewRegistry()
	if n := RegisterExecSkills(toolReg, reg.List()); n != 1 {
		t.Fatalf("expected 1 exec skill registered, got %d", n)
	}
	tt, ok := toolReg.Get("say_hi")
	if !ok {
		t.Fatal("say_hi not callable")
	}
	res, err := tt.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || !strings.Contains(res.Content, "hi-from-disk") {
		t.Errorf("unexpected result: err=%v content=%q", res.IsError, res.Content)
	}
}
