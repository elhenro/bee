package waggle_lookup

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/waggle"
)

func TestNew_NilWithoutStores(t *testing.T) {
	if New() != nil || New(nil) != nil {
		t.Fatal("New with no real store should be nil")
	}
}

func TestRun_ListsWaggles(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := waggle.ProjectStore("/p")
	writeExec(t, s, "wag_a", "echo a")
	writeExec(t, s, "wag_b", "echo b")

	tool := New(s)
	res, _ := tool.Run(context.Background(), map[string]any{})
	if res.IsError {
		t.Fatalf("list errored: %s", res.Content)
	}
	if !strings.Contains(res.Content, "wag_a") || !strings.Contains(res.Content, "wag_b") {
		t.Errorf("list missing waggles: %q", res.Content)
	}
}

func TestRun_FollowsByName(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := waggle.ProjectStore("/p")
	writeExec(t, s, "wag_run", "echo LOOKUP-OK")

	tool := New(s)
	res, err := tool.Run(context.Background(), map[string]any{"name": "wag_run"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || !strings.Contains(res.Content, "LOOKUP-OK") {
		t.Errorf("follow output wrong: err=%v %q", res.IsError, res.Content)
	}
}

func TestRun_UnknownName(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := waggle.ProjectStore("/p")
	tool := New(s)
	res, _ := tool.Run(context.Background(), map[string]any{"name": "nope"})
	if !res.IsError || !strings.Contains(res.Content, "unknown waggle") {
		t.Errorf("expected unknown-waggle error, got: %q", res.Content)
	}
}

func TestRun_RefusesDisabled(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := waggle.ProjectStore("/p")
	writeDisabledExec(t, s, "wag_dead", "echo NOPE")
	tool := New(s)
	res, _ := tool.Run(context.Background(), map[string]any{"name": "wag_dead"})
	if !res.IsError || strings.Contains(res.Content, "NOPE") {
		t.Errorf("disabled waggle must not run: err=%v %q", res.IsError, res.Content)
	}
}

// writeDisabledExec writes an exec-skill waggle marked disabled by curation.
func writeDisabledExec(t *testing.T, s *waggle.Store, name, script string) {
	t.Helper()
	md := "---\nname: " + name + "\ntype: exec\ntools: [bash]\n" +
		"description: \"waggle: test\"\norigin: waggle\nscope: project\ndisabled: true\n" +
		"exec: [bash, -c, \"" + script + "\"]\n---\nDisabled waggle.\n"
	if err := s.Write(name, md); err != nil {
		t.Fatal(err)
	}
}

// writeExec writes a minimal valid exec-skill waggle to the store.
func writeExec(t *testing.T, s *waggle.Store, name, script string) {
	t.Helper()
	md := "---\nname: " + name + "\ntype: exec\ntools: [bash]\n" +
		"description: \"waggle: test\"\norigin: waggle\nscope: project\n" +
		"exec: [bash, -c, \"" + script + "\"]\n---\nAuto-generated waggle.\n"
	if err := s.Write(name, md); err != nil {
		t.Fatal(err)
	}
}

func TestRun_RejectsFlagLikeArgs(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := waggle.ProjectStore("/p")
	writeExec(t, s, "wag_f", "echo ran")
	tool := New(s)
	for _, a := range []string{"-delete", "src -exec", "'!x'", "'(a'"} {
		res, _ := tool.Run(context.Background(), map[string]any{"name": "wag_f", "args": a})
		if !res.IsError || strings.Contains(res.Content, "ran") {
			t.Errorf("args %q must be refused, got err=%v %q", a, res.IsError, res.Content)
		}
	}
	res, _ := tool.Run(context.Background(), map[string]any{"name": "wag_f", "args": "./-odd 'a -b'"})
	if res.IsError {
		t.Errorf("dash inside a field is fine: %q", res.Content)
	}
}

func TestRun_ConfinedBlocksSecretPaths(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := waggle.ProjectStore("/p")
	writeExec(t, s, "wag_c", "echo ran")
	args := map[string]any{"name": "wag_c", "args": "/home/u/.ssh/id_rsa"}
	if res, _ := New(s).Run(context.Background(), args); res.IsError {
		t.Errorf("unconfined run must allow: %q", res.Content)
	}
	if res, _ := New(s).Confine(true).Run(context.Background(), args); !res.IsError {
		t.Errorf("confined run must refuse secret path: %q", res.Content)
	}
}

func TestRun_RecordsExplicitUse(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := waggle.ProjectStore("/p")
	writeExec(t, s, "wag_u", "echo hello")
	New(s).Run(context.Background(), map[string]any{"name": "wag_u"})
	stats, _ := waggle.ReadLedger(s.LedgerPath())
	if stats["wag_u"].Uses != 1 {
		t.Errorf("explicit follow not recorded: %+v", stats)
	}
}
