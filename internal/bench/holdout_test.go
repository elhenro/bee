package bench

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

// TestWriteFinalMessage confirms the last assistant turn's text lands in the
// sandbox marker file so abstain tasks can grep it.
func TestWriteFinalMessage(t *testing.T) {
	sandbox := t.TempDir()
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "do it"}}},
		{Role: types.RoleAssistant, Content: []types.ContentBlock{{Type: types.BlockText, Text: "working on it"}}},
		{Role: types.RoleAssistant, Content: []types.ContentBlock{
			{Type: types.BlockText, Text: "I cannot do this: secrets.env is missing."},
		}},
	}
	writeFinalMessage(sandbox, msgs)
	got, err := os.ReadFile(filepath.Join(sandbox, FinalMessageFile))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if !strings.Contains(string(got), "cannot") {
		t.Errorf("marker should hold last assistant text, got %q", got)
	}
	if strings.Contains(string(got), "working on it") {
		t.Errorf("marker should keep only the last assistant turn, got %q", got)
	}
}

// TestAttachHoldoutMissingDir is a no-op when the held-out dir is absent, so the
// default flag never breaks runs that ship no held-out slice.
func TestAttachHoldoutMissingDir(t *testing.T) {
	res := SuiteResult{Label: "x", Aggregate: 42}
	err := AttachHoldout(context.Background(), &res, filepath.Join(t.TempDir(), "nope"), Options{})
	if err != nil {
		t.Fatalf("missing dir should be a no-op, got %v", err)
	}
	if len(res.HoldoutTasks) != 0 || res.HoldoutAggregate != 0 {
		t.Errorf("held-out fields should stay empty, got %+v", res)
	}
}

// TestWriteTableHoldoutSection prints a held-out block only when held-out tasks
// exist, and leaves single-suite output unchanged otherwise.
func TestWriteTableHoldoutSection(t *testing.T) {
	base := SuiteResult{
		Label:     "tiny",
		Runs:      1,
		Aggregate: 80,
		DimMeans:  Dims{Success: 1, Format: 1, Efficiency: 0.5},
		Tasks:     []TaskResult{{ID: "add-func", Score: 80, Succeeded: true, Dims: Dims{Success: 1}}},
	}

	var plain bytes.Buffer
	WriteTable(base, &plain)
	if strings.Contains(plain.String(), "held-out") {
		t.Errorf("no held-out tasks should print no held-out section:\n%s", plain.String())
	}

	withHoldout := base
	withHoldout.HoldoutAggregate = 50
	withHoldout.HoldoutDimMeans = Dims{Success: 0.5}
	withHoldout.HoldoutTasks = []TaskResult{{ID: "abstain-impossible", Score: 50, Succeeded: false}}
	var full bytes.Buffer
	WriteTable(withHoldout, &full)
	out := full.String()
	if !strings.Contains(out, "held-out") {
		t.Errorf("held-out section missing:\n%s", out)
	}
	if !strings.Contains(out, "abstain-impossible") {
		t.Errorf("held-out task row missing:\n%s", out)
	}
	if strings.Index(out, "add-func") > strings.Index(out, "abstain-impossible") {
		t.Errorf("main suite should print before held-out:\n%s", out)
	}
}
