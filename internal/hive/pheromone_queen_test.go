package hive

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/elhenro/bee/internal/loop"
)

// tailedRunner is a Tailed Runner: it records the tail text as seen at the
// moment Run is called, like an engine evaluating the hook per turn.
type tailedRunner struct {
	mu    sync.Mutex
	tail  func() string
	seen  string
	order int
	out   string
	err   error
}

func (r *tailedRunner) SetPromptTail(f func() string) {
	r.mu.Lock()
	r.tail = f
	r.mu.Unlock()
}

func (r *tailedRunner) Run(_ context.Context, _ string) (loop.RunResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tail != nil {
		r.seen = r.tail()
	}
	return loop.RunResult{FinalText: r.out}, r.err
}

// TestQueen_PheromoneReachesWorkers checks the queen attaches the card to every
// Tailed worker, that a later worker sees an earlier sibling's report line, and
// that Result.Report is filled for clean and dead workers alike.
func TestQueen_PheromoneReachesWorkers(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`[{"role":"builder","task":"build it"},{"role":"sage","task":"check it"},{"role":"builder","task":"doomed"}]`,
		"summary",
	}}
	var seq int
	var seqMu sync.Mutex
	workers := make([]*tailedRunner, 3)
	q := NewQueen(planner, nil)
	q.MaxParallel = 1
	q.WorkerFor = func(idx int, _ SubTask) (Runner, func(error), error) {
		w := &tailedRunner{out: "## report\nstatus: done\ndid: worker " + string(rune('A'+idx)) + " finished"}
		if idx == 2 {
			w.err = errors.New("scripted death")
		}
		orig := w
		workers[idx] = orig
		return runnerFunc(func(ctx context.Context, msg string) (loop.RunResult, error) {
			seqMu.Lock()
			seq++
			orig.order = seq
			seqMu.Unlock()
			return orig.Run(ctx, msg)
		}), nil, nil
	}
	// runnerFunc hides the Tailed interface; wrap so the queen can see it.
	inner := q.WorkerFor
	q.WorkerFor = func(idx int, st SubTask) (Runner, func(error), error) {
		r, done, err := inner(idx, st)
		return tailedWrap{r: r, t: workers[idx]}, done, err
	}

	res, err := q.Run(context.Background(), "the big goal")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i, w := range workers {
		if !strings.Contains(w.seen, "goal: the big goal") || !strings.Contains(w.seen, "## hive contract") {
			t.Errorf("worker %d tail lacks goal/contract:\n%s", i, w.seen)
		}
		if !strings.Contains(w.seen, ", you]") {
			t.Errorf("worker %d tail lacks its own row marker:\n%s", i, w.seen)
		}
	}
	// whichever ran last must see the first finisher's report line.
	var first, last *tailedRunner
	for _, w := range workers {
		if first == nil || w.order < first.order {
			first = w
		}
		if last == nil || w.order > last.order {
			last = w
		}
	}
	if first.err == nil {
		rep, _ := ParseReport(first.out)
		if !strings.Contains(last.seen, "["+rep.Line()+"]") {
			t.Errorf("last worker did not see the first's report line %q:\n%s", rep.Line(), last.seen)
		}
	}
	if got := res.WorkerResults[0].Report; got.Status != ReportDone || got.Did != "worker A finished" {
		t.Errorf("clean worker report = %+v", got)
	}
	if got := res.WorkerResults[2].Report; got.Status != ReportDied || got.Cause != "scripted death" {
		t.Errorf("dead worker report = %+v", got)
	}
	// synthesizer sees the death report, not just the error line.
	synth := planner.prompts[len(planner.prompts)-1]
	if !strings.Contains(synth, "(error: scripted death)") || !strings.Contains(synth, "status: died") {
		t.Errorf("synthesize prompt lacks the death report:\n%s", synth)
	}
	if !strings.Contains(q.Pheromone.Render(-1), "[died: scripted death]") {
		t.Errorf("card after run:\n%s", q.Pheromone.Render(-1))
	}
}

// tailedWrap pairs a Runner with the Tailed sink the test inspects.
type tailedWrap struct {
	r Runner
	t *tailedRunner
}

func (w tailedWrap) Run(ctx context.Context, msg string) (loop.RunResult, error) {
	return w.r.Run(ctx, msg)
}
func (w tailedWrap) SetPromptTail(f func() string) { w.t.SetPromptTail(f) }

// TestQueen_FixedPoolAlsoCarriesCard covers the round-robin Workers path.
func TestQueen_FixedPoolAlsoCarriesCard(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{`["one","two"]`, "summary"}}
	w := &tailedRunner{out: "## report\nstatus: done\ndid: ok"}
	q := NewQueen(planner, []Runner{w})
	if _, err := q.Run(context.Background(), "goal"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(w.seen, "goal: goal") || !strings.Contains(w.seen, "you are builder") {
		t.Errorf("fixed-pool worker tail:\n%s", w.seen)
	}
}

func TestPool_ReportAndCard(t *testing.T) {
	w := &tailedRunner{out: "## report\nstatus: blocked\nnext: needs creds"}
	runnerHook = func(*Worker) Runner { return w }
	defer func() { runnerHook = nil }()

	p := NewPool(2)
	p.Submit(&Worker{ID: "1", Name: "a", Task: "lint a"})
	p.Submit(&Worker{ID: "2", Name: "b", Task: "lint b"})
	results := Collect(p.Run(context.Background()))
	if len(results) != 2 {
		t.Fatalf("results = %d", len(results))
	}
	for _, r := range results {
		if r.Report.Status != ReportBlocked || r.Report.Next != "needs creds" {
			t.Errorf("pool result report = %+v", r.Report)
		}
	}
	if !strings.Contains(w.seen, "lint a") || !strings.Contains(w.seen, "lint b") || strings.Contains(w.seen, "## hive role") {
		t.Errorf("pool tail:\n%s", w.seen)
	}
	if s := Summary(results); !strings.Contains(s, "blocked: needs creds") {
		t.Errorf("Summary lacks the report line:\n%s", s)
	}
}

// TestPool_GoalHeadsCardAndMirrors checks a fan-out with a shared goal shows
// the goal once and labels rows by worker name, and that a card with a path
// is mirrored to disk with every finished report.
func TestPool_GoalHeadsCardAndMirrors(t *testing.T) {
	w := &tailedRunner{out: "## report\nstatus: done\ndid: linted"}
	runnerHook = func(*Worker) Runner { return w }
	defer func() { runnerHook = nil }()

	path := filepath.Join(t.TempDir(), "run.pheromone.md")
	p := NewPool(1)
	p.Goal = "lint every file"
	p.Pheromone = NewPheromone(path)
	p.Submit(&Worker{ID: "1", Name: "a.go", Task: "lint every file on file a.go"})
	p.Submit(&Worker{ID: "2", Name: "b.go", Task: "lint every file on file b.go"})
	if n := len(Collect(p.Run(context.Background()))); n != 2 {
		t.Fatalf("results = %d", n)
	}
	if !strings.Contains(w.seen, "goal: lint every file") || !strings.Contains(w.seen, "2. b.go [") || strings.Contains(w.seen, "on file") {
		t.Errorf("pool tail under a goal:\n%s", w.seen)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"goal: lint every file", "1. a.go [done: linted]", "### bee 1", "### bee 2"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("mirror lacks %q:\n%s", want, data)
		}
	}
}
