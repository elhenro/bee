// Package recipe executes runtime recipes: ordered steps that run in Go (tool,
// shell) or hand one bounded turn to a model (bee). Deterministic steps cost no
// prompt tokens; a bee step sees only its prompt plus whatever earlier outputs
// the template pulls in. Shell steps go through the host's gated bash tool, so
// sandbox and approval apply exactly as they would to a model-issued call.
package recipe

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/elhenro/bee/internal/skills"
)

// Host is what the executor needs from the embedding process.
type Host interface {
	// RunTool runs a registered tool by name. Shell steps call "bash".
	RunTool(ctx context.Context, name string, input map[string]any) (out string, isErr bool, err error)
	// RunBee runs one model turn over prompt, limited to tools when non-empty,
	// and returns the final assistant text.
	RunBee(ctx context.Context, prompt string, tools []string) (string, error)
}

// Status is how a run ended.
type Status string

const (
	StatusOK       Status = "ok"
	StatusEscalate Status = "escalate" // a step routed to the user
	StatusFailed   Status = "failed"   // a step failed with no recovery route
)

// StepResult records one executed step. A step may appear more than once when
// on_failure or next loops back.
type StepResult struct {
	ID     string
	Kind   skills.StepKind
	Output string
	Failed bool
}

// Result is the outcome of one recipe run.
type Result struct {
	Status Status
	Steps  []StepResult
	Output string // output of the last step that ran
	Reason string // set for escalate/failed
}

// Options tunes a run. Log receives one progress line per step (nil = silent).
// MaxSteps caps total step executions so a next/on_failure cycle cannot spin;
// zero means four times the step count.
type Options struct {
	Args     []string
	Log      io.Writer
	MaxSteps int
}

// Run executes a runtime recipe. The returned error covers executor misuse
// (not a runtime recipe); a failing step is reported through Result.Status.
func Run(ctx context.Context, h Host, s skills.Skill, opt Options) (Result, error) {
	if s.Kind != skills.KindRecipe || !s.Runtime {
		return Result{}, fmt.Errorf("recipe: %q is not a runtime recipe", s.Name)
	}
	idx := make(map[string]int, len(s.Steps))
	for i, st := range s.Steps {
		idx[st.ID] = i
	}
	max := opt.MaxSteps
	if max <= 0 {
		max = 4 * len(s.Steps)
	}
	sc := scope{argv: opt.Args, outputs: map[string]string{}}
	var res Result
	for i, n := 0, 0; i < len(s.Steps); n++ {
		if n >= max {
			return finish(res, StatusFailed, fmt.Sprintf("step budget of %d exhausted (cycle?)", max)), nil
		}
		st := s.Steps[i]
		logf(opt.Log, "> %s (%s) %s", st.ID, st.Kind, st.Description)
		out, failed := runStep(ctx, h, st, sc)
		sc.outputs[st.ID] = out
		res.Steps = append(res.Steps, StepResult{ID: st.ID, Kind: st.Kind, Output: out, Failed: failed})
		res.Output = out
		if ctx.Err() != nil {
			return finish(res, StatusFailed, ctx.Err().Error()), nil
		}
		route, reason := st.Next, ""
		if failed {
			logf(opt.Log, "  fail: %s", preview(out))
			route, reason = st.OnFailure, "step "+st.ID+" failed"
			if route == "" {
				return finish(res, StatusFailed, reason), nil
			}
		}
		switch route {
		case "":
			i++
		case "stop":
			return finish(res, StatusOK, ""), nil
		case "escalate":
			if reason == "" {
				reason = "step " + st.ID + " handed back to the user"
			}
			return finish(res, StatusEscalate, reason), nil
		default:
			i = idx[route] // validated at parse time
		}
	}
	return finish(res, StatusOK, ""), nil
}

// runStep executes one step and reports its output plus whether it failed. A
// template or host error is a failure with the error text as output, so the
// recipe's own on_failure routing decides what happens next.
func runStep(ctx context.Context, h Host, st skills.RecipeStep, sc scope) (string, bool) {
	var out string
	var failed bool
	var err error
	switch st.Kind {
	case skills.StepShell:
		var cmd string
		if cmd, err = expand(st.Run, sc, shellQuote); err == nil {
			out, failed, err = h.RunTool(ctx, "bash", map[string]any{"command": cmd})
		}
	case skills.StepTool:
		var args map[string]any
		if args, err = expandArgs(st.Args, sc); err == nil {
			out, failed, err = h.RunTool(ctx, st.Tool, args)
		}
	case skills.StepBee:
		var prompt string
		if prompt, err = expand(st.Prompt, sc, nil); err == nil {
			out, err = h.RunBee(ctx, prompt, st.Tools)
		}
	default:
		err = fmt.Errorf("unknown step kind %q", st.Kind)
	}
	if err != nil {
		return err.Error(), true
	}
	if !failed && st.FailIf != "" {
		if re, rerr := regexp.Compile(st.FailIf); rerr == nil && re.MatchString(out) {
			failed = true
		}
	}
	return out, failed
}

// expandArgs substitutes placeholders in string-valued tool args. Non-string
// values pass through untouched. No quoting: tool args travel as JSON.
func expandArgs(in map[string]any, sc scope) (map[string]any, error) {
	out := make(map[string]any, len(in))
	for k, v := range in {
		s, ok := v.(string)
		if !ok {
			out[k] = v
			continue
		}
		e, err := expand(s, sc, nil)
		if err != nil {
			return nil, err
		}
		out[k] = e
	}
	return out, nil
}

func finish(res Result, st Status, reason string) Result {
	res.Status = st
	res.Reason = reason
	return res
}

func logf(w io.Writer, format string, args ...any) {
	if w != nil {
		fmt.Fprintf(w, format+"\n", args...)
	}
}

func preview(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}
