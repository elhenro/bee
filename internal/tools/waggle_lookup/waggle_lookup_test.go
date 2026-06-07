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
