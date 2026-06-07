package hive

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/loop"
)

func TestParseFindings(t *testing.T) {
	in := "NONE is not it\nFINDING: bug at foo.go:10\nnoise\nfinding: lowercase works\nFINDING:   \n"
	got := parseFindings(in)
	want := []string{"bug at foo.go:10", "lowercase works"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestParseVerdict(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		text string
	}{
		{"CONFIRMED: real bug", true, "real bug"},
		{"REFUTED: false alarm", false, "false alarm"},
		{"refuted - not reproducible", false, "- not reproducible"},
		{"unclear answer", true, ""}, // ambiguous → keep
	}
	for _, c := range cases {
		ok, text := parseVerdict(c.in)
		if ok != c.ok || text != c.text {
			t.Errorf("parseVerdict(%q) = (%v,%q), want (%v,%q)", c.in, ok, text, c.ok, c.text)
		}
	}
}

func TestQueen_ReviewGateThreeDimensionsVerified(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`[{"role":"builder","task":"do it"}]`,
		"final summary",
	}}
	w := &scriptedRunner{outputs: []string{"worker output"}}
	// one reviewer per dimension, each raises a finding
	rc := &scriptedRunner{outputs: []string{"FINDING: correctness issue"}}
	rp := &scriptedRunner{outputs: []string{"FINDING: persistence issue"}}
	ri := &scriptedRunner{outputs: []string{"NONE"}}
	verifier := &scriptedRunner{outputs: []string{
		"CONFIRMED: yes real",
		"CONFIRMED: also real",
	}}

	q := NewQueen(planner, []Runner{w})
	q.Reviewers = []Runner{rc, rp, ri}
	q.Verifier = verifier

	res, err := q.Run(context.Background(), "outer")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Findings) != 2 {
		t.Fatalf("findings = %d, want 2 (%+v)", len(res.Findings), res.Findings)
	}
	for _, f := range res.Findings {
		if !f.Confirmed {
			t.Errorf("finding %+v not confirmed", f)
		}
	}
	// verifier called once per finding (2), not for the NONE dimension
	if len(verifier.prompts) != 2 {
		t.Errorf("verifier saw %d prompts, want 2", len(verifier.prompts))
	}
	// synthesize prompt carries the confirmed findings
	synth := planner.prompts[1]
	for _, want := range []string{"correctness issue", "persistence issue"} {
		if !strings.Contains(synth, want) {
			t.Errorf("synthesize prompt missing %q\nprompt=%s", want, synth)
		}
	}
}

func TestQueen_ReviewGateRefutedDropped(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`[{"role":"builder","task":"do it"}]`,
		"final",
	}}
	w := &scriptedRunner{outputs: []string{"out"}}
	rc := &scriptedRunner{outputs: []string{"FINDING: maybe a bug"}}
	rp := &scriptedRunner{outputs: []string{"NONE"}}
	ri := &scriptedRunner{outputs: []string{"NONE"}}
	verifier := &scriptedRunner{outputs: []string{"REFUTED: false alarm"}}

	q := NewQueen(planner, []Runner{w})
	q.Reviewers = []Runner{rc, rp, ri}
	q.Verifier = verifier

	res, err := q.Run(context.Background(), "t")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Findings) != 1 || res.Findings[0].Confirmed {
		t.Fatalf("want 1 refuted finding, got %+v", res.Findings)
	}
	// refuted finding must NOT reach synthesize
	synth := planner.prompts[1]
	if strings.Contains(synth, "maybe a bug") {
		t.Errorf("refuted finding leaked into synthesize\nprompt=%s", synth)
	}
}

// runnerFunc adapts a func to the Runner interface for one-off behaviors.
type runnerFunc func(context.Context, string) (loop.RunResult, error)

func (f runnerFunc) Run(ctx context.Context, msg string) (loop.RunResult, error) {
	return f(ctx, msg)
}

func TestQueen_ReviewerErrorDegradesNotFatal(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`[{"role":"builder","task":"x"}]`,
		"final",
	}}
	w := &scriptedRunner{outputs: []string{"out"}}
	rc := &scriptedRunner{errAfter: 1} // correctness reviewer errors
	rp := &scriptedRunner{outputs: []string{"NONE"}}
	ri := &scriptedRunner{outputs: []string{"NONE"}}

	q := NewQueen(planner, []Runner{w})
	q.Reviewers = []Runner{rc, rp, ri}

	res, err := q.Run(context.Background(), "t")
	if err != nil {
		t.Fatalf("review error must not fail the turn: %v", err)
	}
	if len(res.Findings) != 1 || !res.Findings[0].Confirmed {
		t.Fatalf("want 1 confirmed gap finding, got %+v", res.Findings)
	}
	if !strings.Contains(res.Findings[0].Claim, "did not complete") {
		t.Errorf("gap finding mislabeled: %q", res.Findings[0].Claim)
	}
	// synthesize still ran
	if res.Final != "final" {
		t.Errorf("Final = %q, want final", res.Final)
	}
}

func TestQueen_VerifierErrorKeepsFindingUnverified(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`[{"role":"builder","task":"x"}]`,
		"final",
	}}
	w := &scriptedRunner{outputs: []string{"out"}}
	rc := &scriptedRunner{outputs: []string{"FINDING: real concern"}}
	rp := &scriptedRunner{outputs: []string{"NONE"}}
	ri := &scriptedRunner{outputs: []string{"NONE"}}
	verifier := &scriptedRunner{errAfter: 1} // verify call errors

	q := NewQueen(planner, []Runner{w})
	q.Reviewers = []Runner{rc, rp, ri}
	q.Verifier = verifier

	res, err := q.Run(context.Background(), "t")
	if err != nil {
		t.Fatalf("verify error must not fail the turn: %v", err)
	}
	if len(res.Findings) != 1 || !res.Findings[0].Confirmed {
		t.Fatalf("unverifiable finding must be kept+confirmed, got %+v", res.Findings)
	}
	if !strings.HasPrefix(res.Findings[0].Verdict, "unverified:") {
		t.Errorf("verdict = %q, want unverified: prefix", res.Findings[0].Verdict)
	}
}

func TestQueen_ReviewCtxCancelAborts(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`[{"role":"builder","task":"x"}]`,
		"final",
	}}
	w := &scriptedRunner{outputs: []string{"out"}}
	ctx, cancel := context.WithCancel(context.Background())
	canceling := runnerFunc(func(c context.Context, _ string) (loop.RunResult, error) {
		cancel()
		return loop.RunResult{}, context.Canceled
	})

	q := NewQueen(planner, []Runner{w})
	q.Reviewers = []Runner{canceling, canceling, canceling}

	_, err := q.Run(ctx, "t")
	if err == nil {
		t.Fatal("ctx cancel during review must abort the run")
	}
}

func TestQueen_ReviewGateSupersedesCritic(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`[{"role":"builder","task":"x"}]`,
		"final",
	}}
	w := &scriptedRunner{outputs: []string{"out"}}
	critic := &scriptedRunner{outputs: []string{"SHOULD_NOT_RUN"}}
	rev := &scriptedRunner{outputs: []string{"NONE", "NONE", "NONE"}}

	q := NewQueen(planner, []Runner{w})
	q.Critic = critic
	q.Reviewers = []Runner{rev} // round-robins across 3 dims

	if _, err := q.Run(context.Background(), "t"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(critic.prompts) != 0 {
		t.Errorf("critic ran despite Reviewers set; prompts=%v", critic.prompts)
	}
}
