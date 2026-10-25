package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/llm"
)

// countingProvider tallies Stream invocations so tests can assert the
// phase-2 tag extractor side-query is or isn't fired.
type countingProvider struct {
	calls atomic.Int32
}

func (c *countingProvider) Name() string { return "counting" }

func (c *countingProvider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	c.calls.Add(1)
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: llm.EventTextDelta, Delta: ""}
	ch <- llm.Event{Type: llm.EventDone}
	close(ch)
	return ch, nil
}

func TestKnowledgeAdapterSkipsOnEmptyDir(t *testing.T) {
	dir := t.TempDir()
	prov := &countingProvider{}
	k := &knowledgeAdapter{
		prov:    prov,
		model:   "test",
		dir:     dir,
		enabled: true,
		topK:    3,
	}
	got, err := k.Query(context.Background(), "any", nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected zero records, got %d", len(got))
	}
	if n := prov.calls.Load(); n != 0 {
		t.Errorf("expected zero provider Stream calls on empty store; got %d", n)
	}
}

// when phase-1 returns ≥2 hits the adapter must not fire the side-LLM.
func TestKnowledgeAdapterSkipsSideQueryWhenPhase1HasHits(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"one.md", "two.md"} {
		body := "---\nname: " + name[:3] + "\ndescription: d\ntags: [misc]\npriority: 3\nexpires: never\n---\nbody"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prov := &countingProvider{}
	k := &knowledgeAdapter{
		prov:    prov,
		model:   "test",
		dir:     dir,
		enabled: true,
		topK:    3,
	}
	got, err := k.Query(context.Background(), "anything", nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 records, got %d", len(got))
	}
	if n := prov.calls.Load(); n != 0 {
		t.Errorf("expected zero side-LLM calls when phase-1 has hits; got %d", n)
	}
}

// guard: the disabled adapter never touches the provider regardless of
// what's on disk.
func TestKnowledgeAdapterDisabledNoCalls(t *testing.T) {
	dir := t.TempDir()
	prov := &countingProvider{}
	k := &knowledgeAdapter{
		prov:    prov,
		dir:     dir,
		enabled: false,
		topK:    3,
	}
	_, _ = k.Query(context.Background(), "any", nil)
	if n := prov.calls.Load(); n != 0 {
		t.Errorf("disabled adapter should never call provider; got %d", n)
	}
}

// thin phase-1 (<2 hits) must not put a model call in front of the turn unless
// [memory] llm_hints opts in.
func TestKnowledgeAdapterThinStoreNoSideQueryByDefault(t *testing.T) {
	dir := t.TempDir()
	body := "---\nname: one\ndescription: d\ntags: [misc]\npriority: 3\nexpires: never\n---\nbody"
	if err := os.WriteFile(filepath.Join(dir, "one.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	prov := &countingProvider{}
	k := &knowledgeAdapter{prov: prov, model: "test", dir: dir, enabled: true, topK: 3}
	if _, err := k.Query(context.Background(), "anything", nil); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if n := prov.calls.Load(); n != 0 {
		t.Errorf("llm_hints off: expected zero side-LLM calls; got %d", n)
	}
}

// opted in, a side model that never answers is cut at hintBudget, not waited on.
func TestKnowledgeAdapterHintsBoundedByBudget(t *testing.T) {
	dir := t.TempDir()
	body := "---\nname: one\ndescription: d\ntags: [misc]\npriority: 3\nexpires: never\n---\nbody"
	if err := os.WriteFile(filepath.Join(dir, "one.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	k := &knowledgeAdapter{prov: hangingProvider{}, model: "test", dir: dir, enabled: true, hints: true, topK: 3}
	start := time.Now()
	if _, err := k.Query(context.Background(), "anything", nil); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if d := time.Since(start); d > 3*hintBudget {
		t.Errorf("Query blocked %v on a hung side model, budget is %v", d, hintBudget)
	}
}

type hangingProvider struct{}

func (hangingProvider) Name() string { return "hanging" }

func (hangingProvider) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Event, error) {
	ch := make(chan llm.Event)
	go func() {
		<-ctx.Done()
		ch <- llm.Event{Type: llm.EventError, Err: ctx.Err()}
		close(ch)
	}()
	return ch, nil
}
