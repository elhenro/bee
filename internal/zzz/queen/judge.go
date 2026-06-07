package queen

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// Candidate is one worker branch handed to the judge: its 1-based label and
// the full diff of its work against the base branch.
type Candidate struct {
	Idx   int // index into the worker slice
	Label string
	Diff  string
}

// Verdict is the judge's pick.
type Verdict struct {
	WinnerIdx int    // worker index of the winning candidate
	Reason    string // short justification
}

// judgeSystem instructs the diff judge. Mirrors the goal-eval protocol: let a
// small reasoning model narrate, then demand one terminal line trivial to
// parse. The judge sees only diffs — no files, no commands.
const judgeSystem = `You are a strict code reviewer choosing the single best solution among
several independent attempts at the SAME objective. You see only each attempt's
git diff — no other context. Judge by: does it actually achieve the objective,
is it correct, is it focused (no unrelated churn), is it complete.

Be decisive. Prefer a correct, complete, minimal diff over a large speculative one.
An empty or trivial diff that ignores the objective loses.

The LAST line of your reply MUST be exactly:
WINNER: <n> — <=15 word reason
where <n> is the candidate number. Reason briefly before that line if needed,
but the final line must start with WINNER:.`

// judgeMaxDiffBytes caps each candidate diff fed to the judge.
const judgeMaxDiffBytes = 6000

// judgeMaxTokens caps the judge's reply — narration plus the verdict line.
const judgeMaxTokens = 640

// Judge asks a fast model to pick the best candidate. Single cheap side call,
// no tools. With 0 or 1 candidate it short-circuits. On any provider/parse
// error it returns the first candidate as a safe default plus the error.
func Judge(ctx context.Context, p llm.Provider, model, objective string, cands []Candidate) (Verdict, error) {
	switch {
	case len(cands) == 0:
		return Verdict{WinnerIdx: -1, Reason: "no candidates"}, nil
	case len(cands) == 1:
		return Verdict{WinnerIdx: cands[0].Idx, Reason: "only candidate"}, nil
	case p == nil:
		return Verdict{WinnerIdx: cands[0].Idx, Reason: "no provider"}, nil
	}

	user := buildJudgePrompt(objective, cands)
	req := llm.Request{
		Model:  model,
		System: judgeSystem,
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: user}}},
		},
		MaxTokens:   judgeMaxTokens,
		Temperature: 0,
		Stream:      true,
	}
	ch, err := p.Stream(ctx, req)
	if err != nil {
		return Verdict{WinnerIdx: cands[0].Idx, Reason: "judge call failed"}, err
	}
	var buf strings.Builder
	var streamErr error
	for ev := range ch {
		switch ev.Type {
		case llm.EventTextDelta:
			buf.WriteString(ev.Delta)
		case llm.EventError:
			if ev.Err != nil {
				streamErr = ev.Err
			}
		}
	}
	raw := strings.TrimSpace(buf.String())
	if raw == "" {
		return Verdict{WinnerIdx: cands[0].Idx, Reason: "empty judge response"}, streamErr
	}
	pick, reason, ok := parseWinner(raw, len(cands))
	if !ok {
		return Verdict{WinnerIdx: cands[0].Idx, Reason: "unparseable verdict; defaulted to #1"}, nil
	}
	return Verdict{WinnerIdx: cands[pick].Idx, Reason: reason}, nil
}

// buildJudgePrompt renders the objective + numbered candidate diffs.
func buildJudgePrompt(objective string, cands []Candidate) string {
	var b strings.Builder
	b.WriteString("OBJECTIVE:\n")
	b.WriteString(strings.TrimSpace(objective))
	b.WriteString("\n\nCANDIDATES:\n")
	for i, c := range cands {
		fmt.Fprintf(&b, "\n=== Candidate %d (%s) ===\n", i+1, c.Label)
		d := strings.TrimSpace(c.Diff)
		if d == "" {
			d = "(empty diff — no changes)"
		}
		b.WriteString(d)
		b.WriteString("\n")
	}
	return b.String()
}

// parseWinner finds the last "WINNER: <n>" marker and returns the 0-based slot
// into the candidate slice (n is 1-based in the protocol).
func parseWinner(raw string, n int) (slot int, reason string, ok bool) {
	const marker = "winner:"
	low := strings.ToLower(raw)
	i := strings.LastIndex(low, marker)
	if i < 0 {
		return 0, "", false
	}
	rest := strings.TrimSpace(raw[i+len(marker):])
	// leading number = candidate index
	numEnd := 0
	for numEnd < len(rest) && rest[numEnd] >= '0' && rest[numEnd] <= '9' {
		numEnd++
	}
	if numEnd == 0 {
		return 0, "", false
	}
	num, err := strconv.Atoi(rest[:numEnd])
	if err != nil || num < 1 || num > n {
		return 0, "", false
	}
	reason = strings.TrimSpace(strings.TrimLeft(firstLineOf(rest[numEnd:]), "—-: "))
	return num - 1, reason, true
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
