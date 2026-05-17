package hive

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/elhenro/bee/internal/loop"
)

// scriptedRunner returns canned outputs in order. Records every prompt it sees
// so tests can assert what the queen sent.
type scriptedRunner struct {
	mu       sync.Mutex
	prompts  []string
	outputs  []string
	idx      int32
	errAfter int // if >0, return error on prompt index errAfter-1
	name     string
}

func (s *scriptedRunner) Run(_ context.Context, msg string) (loop.RunResult, error) {
	s.mu.Lock()
	s.prompts = append(s.prompts, msg)
	s.mu.Unlock()
	i := atomic.AddInt32(&s.idx, 1) - 1
	if s.errAfter > 0 && int(i) == s.errAfter-1 {
		return loop.RunResult{}, errors.New("scripted failure")
	}
	if int(i) >= len(s.outputs) {
		return loop.RunResult{FinalText: ""}, nil
	}
	return loop.RunResult{FinalText: s.outputs[i]}, nil
}

func TestQueen_DecomposeParsesJSON(t *testing.T) {
	planner := &scriptedRunner{
		outputs: []string{
			`["analyze", "draft", "review"]`,
			"final synthesis",
		},
	}
	w := &scriptedRunner{outputs: []string{"a-out", "d-out", "r-out"}}

	q := NewQueen(planner, []Runner{w})
	res, err := q.Run(context.Background(), "do the thing")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"analyze", "draft", "review"}
	if len(res.Plan) != 3 {
		t.Fatalf("plan len = %d, want 3 (plan=%v)", len(res.Plan), res.Plan)
	}
	for i, p := range want {
		if res.Plan[i].Task != p {
			t.Errorf("Plan[%d].Task = %q, want %q", i, res.Plan[i].Task, p)
		}
	}
	if res.Final != "final synthesis" {
		t.Errorf("Final = %q, want %q", res.Final, "final synthesis")
	}
}

func TestQueen_DispatchesToAllWorkers(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`["t1", "t2", "t3", "t4"]`,
		"summary",
	}}
	w1 := &scriptedRunner{outputs: []string{"w1-a", "w1-b"}, name: "w1"}
	w2 := &scriptedRunner{outputs: []string{"w2-a", "w2-b"}, name: "w2"}

	q := NewQueen(planner, []Runner{w1, w2})
	res, err := q.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.WorkerResults) != 4 {
		t.Fatalf("worker results = %d, want 4", len(res.WorkerResults))
	}
	// round-robin: indices 0,2 → w1; indices 1,3 → w2.
	if len(w1.prompts) != 2 {
		t.Errorf("w1 saw %d prompts, want 2 (prompts=%v)", len(w1.prompts), w1.prompts)
	}
	if len(w2.prompts) != 2 {
		t.Errorf("w2 saw %d prompts, want 2", len(w2.prompts))
	}
	// each worker result should have non-empty Final
	for i, r := range res.WorkerResults {
		if r.Final == "" {
			t.Errorf("worker result %d has empty Final", i)
		}
	}
}

func TestQueen_SynthesisSeesWorkerOutputs(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`["alpha", "beta"]`,
		"done",
	}}
	w := &scriptedRunner{outputs: []string{"ALPHA_RESULT", "BETA_RESULT"}}

	q := NewQueen(planner, []Runner{w})
	_, err := q.Run(context.Background(), "outer task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// planner.prompts: [0] decompose, [1] synthesize
	if len(planner.prompts) != 2 {
		t.Fatalf("planner saw %d prompts, want 2", len(planner.prompts))
	}
	synth := planner.prompts[1]
	for _, want := range []string{"ALPHA_RESULT", "BETA_RESULT", "alpha", "beta", "outer task"} {
		if !strings.Contains(synth, want) {
			t.Errorf("synthesis prompt missing %q\nprompt=%s", want, synth)
		}
	}
}

func TestQueen_ParsesFencedJSON(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		"Here's the plan:\n```json\n[\"one\", \"two\"]\n```\n",
		"summary",
	}}
	w := &scriptedRunner{outputs: []string{"a", "b"}}
	q := NewQueen(planner, []Runner{w})
	res, err := q.Run(context.Background(), "t")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Plan) != 2 || res.Plan[0].Task != "one" || res.Plan[1].Task != "two" {
		t.Errorf("Plan = %v, want [one two]", res.Plan)
	}
}

func TestQueen_FallbackOnBadJSON(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		"I have no idea what to do here.",
		"done somehow",
	}}
	w := &scriptedRunner{outputs: []string{"ok"}}
	q := NewQueen(planner, []Runner{w})
	res, err := q.Run(context.Background(), "original task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// fallback: single task = original
	if len(res.Plan) != 1 || res.Plan[0].Task != "original task" {
		t.Errorf("expected fallback plan [original task], got %v", res.Plan)
	}
	if len(res.WorkerResults) != 1 || res.WorkerResults[0].Final != "ok" {
		t.Errorf("worker result wrong: %+v", res.WorkerResults)
	}
}

func TestQueen_CapsAtMaxSubTasks(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`["1","2","3","4","5","6","7","8","9","10"]`,
		"summary",
	}}
	// 10 workers so plenty for fan-out
	workers := make([]Runner, 10)
	for i := range workers {
		workers[i] = &scriptedRunner{outputs: []string{"x"}}
	}
	q := NewQueen(planner, workers)
	res, err := q.Run(context.Background(), "big task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Plan) != MaxSubTasks {
		t.Errorf("plan len = %d, want MaxSubTasks=%d", len(res.Plan), MaxSubTasks)
	}
}

func TestQueen_PropagatesWorkerError(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`["a","b"]`,
		"unused",
	}}
	w := &scriptedRunner{outputs: []string{"ok"}, errAfter: 2} // second call errors
	q := NewQueen(planner, []Runner{w})
	res, err := q.Run(context.Background(), "task")
	if err == nil {
		t.Fatalf("expected error, got nil; res=%+v", res)
	}
}

func TestQueen_NilPlanner(t *testing.T) {
	q := &Queen{Workers: []Runner{&scriptedRunner{}}}
	if _, err := q.Run(context.Background(), "t"); err == nil {
		t.Error("expected error with nil planner")
	}
}

func TestQueen_NoWorkers(t *testing.T) {
	q := &Queen{Planner: &scriptedRunner{}}
	if _, err := q.Run(context.Background(), "t"); err == nil {
		t.Error("expected error with no workers")
	}
}

func TestQueen_DecomposeAssignsRoles(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`[{"role":"forager","task":"find every file that imports X"},` +
			`{"role":"builder","task":"add Y to file Z"},` +
			`{"role":"critic","task":"review the plan"}]`,
		"final",
	}}
	w := &scriptedRunner{outputs: []string{"a", "b", "c"}}

	q := NewQueen(planner, []Runner{w})
	res, err := q.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []SubTask{
		{Role: RoleForager, Task: "find every file that imports X"},
		{Role: RoleBuilder, Task: "add Y to file Z"},
		{Role: RoleCritic, Task: "review the plan"},
	}
	if len(res.Plan) != len(want) {
		t.Fatalf("plan len = %d, want %d (plan=%v)", len(res.Plan), len(want), res.Plan)
	}
	for i, w := range want {
		if res.Plan[i].Role != w.Role || res.Plan[i].Task != w.Task {
			t.Errorf("Plan[%d] = %+v, want %+v", i, res.Plan[i], w)
		}
	}
}

func TestQueen_DecomposeAcceptsLegacyArray(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`["a","b","c"]`,
		"final",
	}}
	w := &scriptedRunner{outputs: []string{"x", "y", "z"}}

	q := NewQueen(planner, []Runner{w})
	res, err := q.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Plan) != 3 {
		t.Fatalf("plan len = %d, want 3", len(res.Plan))
	}
	for i, st := range res.Plan {
		if st.Role != RoleBuilder {
			t.Errorf("Plan[%d].Role = %q, want %q", i, st.Role, RoleBuilder)
		}
	}
}

func TestQueen_DecomposeUnknownRoleFallsBackToBuilder(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`[{"role":"frobnicate","task":"x"}]`,
		"final",
	}}
	w := &scriptedRunner{outputs: []string{"out"}}

	q := NewQueen(planner, []Runner{w})
	res, err := q.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Plan) != 1 {
		t.Fatalf("plan len = %d, want 1", len(res.Plan))
	}
	if res.Plan[0].Role != RoleBuilder {
		t.Errorf("Plan[0].Role = %q, want %q", res.Plan[0].Role, RoleBuilder)
	}
	if res.Plan[0].Task != "x" {
		t.Errorf("Plan[0].Task = %q, want %q", res.Plan[0].Task, "x")
	}
}

func TestQueen_WithCriticAppendsToSynthesize(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`[{"role":"builder","task":"do the thing"}]`,
		"final summary",
	}}
	w := &scriptedRunner{outputs: []string{"worker output"}}
	critic := &scriptedRunner{outputs: []string{"CRITIQUE_SENTINEL_VALUE"}}

	q := NewQueen(planner, []Runner{w})
	q.Critic = critic

	res, err := q.Run(context.Background(), "outer")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Critique != "CRITIQUE_SENTINEL_VALUE" {
		t.Errorf("Critique = %q, want CRITIQUE_SENTINEL_VALUE", res.Critique)
	}
	if len(critic.prompts) != 1 {
		t.Fatalf("critic saw %d prompts, want 1", len(critic.prompts))
	}
	// planner.prompts[1] is the synthesize prompt; must contain the critique
	if len(planner.prompts) != 2 {
		t.Fatalf("planner saw %d prompts, want 2", len(planner.prompts))
	}
	synth := planner.prompts[1]
	if !strings.Contains(synth, "CRITIQUE_SENTINEL_VALUE") {
		t.Errorf("synthesize prompt missing critique\nprompt=%s", synth)
	}
	if !strings.Contains(synth, "Critic review") {
		t.Errorf("synthesize prompt missing 'Critic review' header\nprompt=%s", synth)
	}
}

func TestQueen_NilCriticSkipsReview(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`[{"role":"builder","task":"t"}]`,
		"final",
	}}
	w := &scriptedRunner{outputs: []string{"out"}}

	q := NewQueen(planner, []Runner{w}) // Critic left nil
	res, err := q.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Critique != "" {
		t.Errorf("Critique = %q, want empty", res.Critique)
	}
	// no critique → no Critic review header in synthesize prompt
	if len(planner.prompts) < 2 {
		t.Fatalf("planner prompts = %d, want >=2", len(planner.prompts))
	}
	if strings.Contains(planner.prompts[1], "Critic review") {
		t.Errorf("expected no 'Critic review' header in synthesize prompt\nprompt=%s", planner.prompts[1])
	}
}
