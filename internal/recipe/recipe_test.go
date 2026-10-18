package recipe

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/skills"
)

// fakeHost records calls and answers from a script keyed by command/prompt.
type fakeHost struct {
	calls  []string
	fail   map[string]bool // tool outputs flagged IsError
	beeErr error
}

func (f *fakeHost) RunTool(_ context.Context, name string, in map[string]any) (string, bool, error) {
	cmd, _ := in["command"].(string)
	if cmd == "" {
		cmd = name + ":" + in["path"].(string)
	}
	f.calls = append(f.calls, cmd)
	return "out(" + cmd + ")", f.fail[cmd], nil
}

func (f *fakeHost) RunBee(_ context.Context, prompt string, tools []string) (string, error) {
	f.calls = append(f.calls, "bee:"+prompt+"|"+strings.Join(tools, ","))
	if f.beeErr != nil {
		return "", f.beeErr
	}
	return "answer(" + prompt + ")", nil
}

func parse(t *testing.T, src string) skills.Skill {
	t.Helper()
	s, err := skills.Parse("r.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const happy = `---
name: r
type: recipe
steps:
  - id: list
    kind: shell
    description: list
    run: ls {{argv.0}}
  - id: pick
    kind: bee
    description: pick
    tools: [read]
    prompt: "choose from {{list}}"
  - id: show
    kind: tool
    description: show
    tool: read
    args: {path: "{{pick}}"}
---
`

func TestRun_SequentialTemplatesAndQuoting(t *testing.T) {
	h := &fakeHost{}
	res, err := Run(context.Background(), h, parse(t, happy), Options{Args: []string{"a b"}})
	if err != nil || res.Status != StatusOK {
		t.Fatalf("err=%v status=%s reason=%s", err, res.Status, res.Reason)
	}
	want := []string{
		"ls 'a b'",
		"bee:choose from out(ls 'a b')|read",
		"read:answer(choose from out(ls 'a b'))",
	}
	if strings.Join(h.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(h.calls, "\n"), strings.Join(want, "\n"))
	}
	if !strings.HasPrefix(res.Output, "out(read:") || len(res.Steps) != 3 {
		t.Fatalf("result: %+v", res)
	}
}

const branching = `---
name: r
type: recipe
steps:
  - id: test
    kind: shell
    description: test
    run: go test
    on_failure: explain
    next: commit
  - id: explain
    kind: bee
    description: explain
    prompt: "why {{test}}"
    next: escalate
  - id: commit
    kind: shell
    description: commit
    run: git commit
---
`

func TestRun_NextSkipsRecoveryStep(t *testing.T) {
	h := &fakeHost{}
	res, _ := Run(context.Background(), h, parse(t, branching), Options{})
	if res.Status != StatusOK || strings.Join(h.calls, ",") != "go test,git commit" {
		t.Fatalf("status=%s calls=%v", res.Status, h.calls)
	}
}

func TestRun_OnFailureRoutesThenEscalates(t *testing.T) {
	h := &fakeHost{fail: map[string]bool{"go test": true}}
	res, _ := Run(context.Background(), h, parse(t, branching), Options{})
	if res.Status != StatusEscalate {
		t.Fatalf("status=%s reason=%s", res.Status, res.Reason)
	}
	if len(h.calls) != 2 || !strings.HasPrefix(h.calls[1], "bee:why out(go test)") {
		t.Fatalf("calls=%v", h.calls)
	}
	if !res.Steps[0].Failed || res.Steps[1].Failed {
		t.Fatalf("steps=%+v", res.Steps)
	}
}

func TestRun_FailureWithoutRouteFails(t *testing.T) {
	src := "---\nname: r\ntype: recipe\nsteps:\n  - id: a\n    kind: shell\n    description: a\n    run: boom\n  - id: b\n    kind: shell\n    description: b\n    run: never\n---\n"
	h := &fakeHost{fail: map[string]bool{"boom": true}}
	res, _ := Run(context.Background(), h, parse(t, src), Options{})
	if res.Status != StatusFailed || len(h.calls) != 1 {
		t.Fatalf("status=%s calls=%v", res.Status, h.calls)
	}
}

func TestRun_OnFailureAbortAndPaddedRef(t *testing.T) {
	abort := "---\nname: r\ntype: recipe\nsteps:\n  - id: a\n    kind: shell\n    description: a\n    run: boom\n    on_failure: abort\n  - id: b\n    kind: shell\n    description: b\n    run: never\n---\n"
	h := &fakeHost{fail: map[string]bool{"boom": true}}
	res, _ := Run(context.Background(), h, parse(t, abort), Options{})
	if res.Status != StatusFailed || len(h.calls) != 1 {
		t.Fatalf("abort: status=%s calls=%v", res.Status, h.calls)
	}
	padded := "---\nname: r\ntype: recipe\nsteps:\n  - id: a\n    kind: shell\n    description: a\n    run: boom\n    on_failure: \" fix \"\n    next: stop\n  - id: fix\n    kind: shell\n    description: f\n    run: repair\n---\n"
	h = &fakeHost{fail: map[string]bool{"boom": true}}
	res, _ = Run(context.Background(), h, parse(t, padded), Options{})
	if res.Status != StatusOK || strings.Join(h.calls, ",") != "boom,repair" {
		t.Fatalf("padded: status=%s calls=%v", res.Status, h.calls)
	}
}

func TestRun_FailIfBranchesOnBeeAnswer(t *testing.T) {
	src := "---\nname: r\ntype: recipe\nsteps:\n  - id: judge\n    kind: bee\n    description: j\n    prompt: ok?\n    fail_if: answer\n    on_failure: escalate\n  - id: go\n    kind: shell\n    description: g\n    run: deploy\n---\n"
	h := &fakeHost{}
	res, _ := Run(context.Background(), h, parse(t, src), Options{})
	if res.Status != StatusEscalate || len(h.calls) != 1 {
		t.Fatalf("status=%s calls=%v", res.Status, h.calls)
	}
}

func TestRun_BeeErrorIsStepFailure(t *testing.T) {
	src := "---\nname: r\ntype: recipe\nsteps:\n  - id: j\n    kind: bee\n    description: j\n    prompt: hi\n---\n"
	h := &fakeHost{beeErr: errors.New("escalate: stuck")}
	res, _ := Run(context.Background(), h, parse(t, src), Options{})
	if res.Status != StatusFailed || !strings.Contains(res.Output, "stuck") {
		t.Fatalf("res=%+v", res)
	}
}

func TestRun_UnknownPlaceholderFailsStep(t *testing.T) {
	src := "---\nname: r\ntype: recipe\nsteps:\n  - id: a\n    kind: shell\n    description: a\n    run: echo {{nope}}\n---\n"
	h := &fakeHost{}
	res, _ := Run(context.Background(), h, parse(t, src), Options{})
	if res.Status != StatusFailed || len(h.calls) != 0 || !strings.Contains(res.Output, "{{nope}}") {
		t.Fatalf("res=%+v calls=%v", res, h.calls)
	}
}

func TestRun_CycleGuard(t *testing.T) {
	src := "---\nname: r\ntype: recipe\nsteps:\n  - id: a\n    kind: shell\n    description: a\n    run: spin\n    next: a\n---\n"
	h := &fakeHost{}
	res, _ := Run(context.Background(), h, parse(t, src), Options{MaxSteps: 3})
	if res.Status != StatusFailed || len(h.calls) != 3 {
		t.Fatalf("status=%s calls=%d", res.Status, len(h.calls))
	}
}

func TestRun_RejectsLegacyRecipe(t *testing.T) {
	src := "---\nname: r\ntype: recipe\nsteps:\n  - id: a\n    description: model walks this\n---\n"
	if _, err := Run(context.Background(), &fakeHost{}, parse(t, src), Options{}); err == nil {
		t.Fatal("expected error for non-runtime recipe")
	}
}

func TestExpand_ArgsAndQuote(t *testing.T) {
	sc := scope{argv: []string{"x", "it's"}, outputs: map[string]string{"s-1": "v"}}
	got, err := expand("{{args}}|{{ argv.1 }}|{{s-1}}", sc, shellQuote)
	if err != nil || got != `'x it'\''s'|'it'\''s'|'v'` {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := expand("{{argv.9}}", sc, nil); err == nil {
		t.Fatal("out-of-range argv should error")
	}
}
