// Queen-and-workers orchestration.
//
// The Queen runs three or four phases:
//  1. decompose: ask Planner Runner to split the task into ≤8 sub-tasks (JSON
//     objects pairing role + task).
//  2. dispatch:  round-robin sub-tasks to Workers, fan out via goroutines.
//  3. review:    (optional) hand worker outputs to Critic for a short critique.
//  4. synthesize: hand all worker outputs (plus critique if any) back to
//     Planner for a final summary.
//
// Pool from spawn.go (slice 4A) is preferred when available; this file uses
// direct goroutine fan-out so the queen still builds even if 4A is missing.
package hive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MaxSubTasks caps how many sub-tasks the queen will accept from the planner.
// matches the prompt budget; defends against runaway plans.
const MaxSubTasks = 8

// Queen orchestrates a planner Runner and N worker Runners. Critic is optional;
// when set, its output is appended to the synthesize prompt.
type Queen struct {
	Planner     Runner
	Workers     []Runner
	Critic      Runner
	MaxParallel int // 0 => len(Workers)
}

// QueenResult is the aggregate of one Queen.Run.
type QueenResult struct {
	Plan          []SubTask
	WorkerResults []Result
	Critique      string
	Final         string
}

// NewQueen returns a Queen ready to Run. MaxParallel defaults to len(workers).
func NewQueen(planner Runner, workers []Runner) *Queen {
	return &Queen{Planner: planner, Workers: workers, MaxParallel: len(workers)}
}

// Run executes the full decompose → dispatch → (review) → synthesize pipeline.
func (q *Queen) Run(ctx context.Context, task string) (QueenResult, error) {
	if q.Planner == nil {
		return QueenResult{}, errors.New("queen: planner is nil")
	}
	if len(q.Workers) == 0 {
		return QueenResult{}, errors.New("queen: no workers")
	}

	plan, err := q.decompose(ctx, task)
	if err != nil {
		return QueenResult{}, fmt.Errorf("queen: decompose: %w", err)
	}
	if len(plan) == 0 {
		// fallback: planner returned nothing useful; treat as single-task.
		plan = []SubTask{{Role: RoleBuilder, Task: task}}
	}

	results, err := q.dispatch(ctx, plan)
	if err != nil {
		return QueenResult{Plan: plan, WorkerResults: results}, err
	}

	var critique string
	if q.Critic != nil {
		critique, err = q.review(ctx, task, plan, results)
		if err != nil {
			return QueenResult{Plan: plan, WorkerResults: results}, fmt.Errorf("queen: review: %w", err)
		}
	}

	final, err := q.synthesize(ctx, task, plan, results, critique)
	if err != nil {
		return QueenResult{Plan: plan, WorkerResults: results, Critique: critique}, fmt.Errorf("queen: synthesize: %w", err)
	}
	return QueenResult{Plan: plan, WorkerResults: results, Critique: critique, Final: final}, nil
}

// decompose asks the planner to split task into 2-8 independent sub-tasks
// emitted as a JSON array of {role, task} objects. Legacy string arrays are
// still accepted for backward compatibility.
func (q *Queen) decompose(ctx context.Context, task string) ([]SubTask, error) {
	prompt := fmt.Sprintf(
		"Decompose this task into 2-8 independent sub-tasks. "+
			"Return a JSON array of objects with shape "+
			`{"role": "<role>", "task": "<sub-task>"}. `+
			"Valid roles: %s. "+
			"Pick the role that best fits each sub-task. "+
			"Task: %s",
		strings.Join(roleNamesCSV(), ", "), task,
	)
	out, err := q.Planner.Run(ctx, prompt)
	if err != nil {
		return nil, err
	}
	plan := parseSubTasks(out.FinalText)
	if len(plan) > MaxSubTasks {
		plan = plan[:MaxSubTasks]
	}
	return plan, nil
}

// dispatch fans plan out across workers round-robin and waits for all to
// finish, honoring ctx cancellation. Returns partial results on first error.
func (q *Queen) dispatch(ctx context.Context, plan []SubTask) ([]Result, error) {
	results := make([]Result, len(plan))
	parallel := q.MaxParallel
	if parallel <= 0 || parallel > len(q.Workers) {
		parallel = len(q.Workers)
	}
	sem := make(chan struct{}, parallel)

	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once

	for i, sub := range plan {
		worker := q.Workers[i%len(q.Workers)]
		wg.Add(1)
		go func(idx int, w Runner, st SubTask) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-subCtx.Done():
				results[idx] = Result{
					WorkerID: fmt.Sprintf("w%d", idx),
					Name:     fmt.Sprintf("worker-%d", idx),
					Task:     st.Task,
					Err:      subCtx.Err(),
				}
				return
			}
			defer func() { <-sem }()

			started := time.Now().UTC()
			out, err := w.Run(subCtx, st.Task)
			ended := time.Now().UTC()
			r := Result{
				WorkerID: fmt.Sprintf("w%d", idx),
				Name:     fmt.Sprintf("worker-%d", idx),
				Task:     st.Task,
				Started:  started,
				Ended:    ended,
			}
			if err != nil {
				r.Err = err
				errOnce.Do(func() { firstErr = err; cancel() })
			} else {
				r.Final = out.FinalText
			}
			results[idx] = r
		}(i, worker, sub)
	}

	wg.Wait()
	if firstErr != nil {
		return results, firstErr
	}
	if ctx.Err() != nil {
		return results, ctx.Err()
	}
	return results, nil
}

// review asks the Critic to read the plan + worker outputs and emit a short
// critique. Critic is opt-in via Queen.Critic.
func (q *Queen) review(ctx context.Context, task string, plan []SubTask, results []Result) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b,
		"Review this hive run. Read the original task, the plan, and each worker's result. "+
			"Emit a 1-2 paragraph critique: missed edge cases, weak spots, "+
			"and what's still uncertain. No fixes, no code.\n\n"+
			"Original task: %s\n\n",
		task,
	)
	writePlanAndResults(&b, plan, results)
	out, err := q.Critic.Run(ctx, b.String())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.FinalText), nil
}

// synthesize hands all worker outputs back to the planner for a cohesive
// final summary. If critique is non-empty, it is appended verbatim.
func (q *Queen) synthesize(ctx context.Context, task string, plan []SubTask, results []Result, critique string) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b,
		"Synthesize these %d sub-task results into one cohesive summary. "+
			"Sub-tasks (with assigned roles) and results follow.\n\n"+
			"Original task: %s\n\n",
		len(results), task,
	)
	writePlanAndResults(&b, plan, results)
	if critique != "" {
		b.WriteString("### Critic review\n")
		b.WriteString(critique)
		b.WriteString("\n\n")
	}
	out, err := q.Planner.Run(ctx, b.String())
	if err != nil {
		return "", err
	}
	return out.FinalText, nil
}

// writePlanAndResults renders the plan + worker outputs into a shared format
// used by both review and synthesize.
func writePlanAndResults(b *strings.Builder, plan []SubTask, results []Result) {
	for i, r := range results {
		var st SubTask
		if i < len(plan) {
			st = plan[i]
		}
		role := string(st.Role)
		if role == "" {
			role = string(RoleBuilder)
		}
		fmt.Fprintf(b, "## Sub-task %d (role: %s)\n%s\n\n### Result\n", i+1, role, st.Task)
		if r.Err != nil {
			fmt.Fprintf(b, "(error: %v)\n\n", r.Err)
			continue
		}
		b.WriteString(strings.TrimSpace(r.Final))
		b.WriteString("\n\n")
	}
}

// roleNamesCSV returns role names in stable order for prompt embedding.
func roleNamesCSV() []string {
	roles := AllRoles()
	out := make([]string, len(roles))
	for i, r := range roles {
		out[i] = string(r)
	}
	return out
}

// parseSubTasks extracts a JSON array from the planner output. Two shapes are
// tolerated:
//
//   - []string (legacy)             → each entry becomes a SubTask with RoleBuilder
//   - []{role, task} (new)          → role is parsed via ParseRole; unknown → RoleBuilder
//
// Fenced code blocks and prose wrappers are stripped before parsing.
func parseSubTasks(s string) []SubTask {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	s = stripFence(s)
	body := extractArray(s)
	if body == "" {
		return nil
	}

	// try object form first
	var objs []struct {
		Role string `json:"role"`
		Task string `json:"task"`
	}
	if err := json.Unmarshal([]byte(body), &objs); err == nil && len(objs) > 0 {
		out := make([]SubTask, 0, len(objs))
		for _, o := range objs {
			task := strings.TrimSpace(o.Task)
			if task == "" {
				continue
			}
			role, ok := ParseRole(o.Role)
			if !ok {
				role = RoleBuilder
			}
			out = append(out, SubTask{Role: role, Task: task})
		}
		if len(out) > 0 {
			return out
		}
	}

	// fallback: legacy string array
	var arr []string
	if err := json.Unmarshal([]byte(body), &arr); err == nil {
		out := make([]SubTask, 0, len(arr))
		for _, x := range arr {
			x = strings.TrimSpace(x)
			if x == "" {
				continue
			}
			out = append(out, SubTask{Role: RoleBuilder, Task: x})
		}
		return out
	}
	return nil
}

// extractArray returns the first [...] block in s, or s itself if it already
// parses as JSON. Empty string means no array found.
func extractArray(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		return s
	}
	start := strings.IndexByte(s, '[')
	end := strings.LastIndexByte(s, ']')
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// drop first line (fence + optional lang)
	nl := strings.IndexByte(s, '\n')
	if nl < 0 {
		return s
	}
	s = s[nl+1:]
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
