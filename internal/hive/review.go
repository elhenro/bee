// Review gate: the queen's last quality pass after workers finish changing
// code, before the result is handed back (and the user commits).
//
// Three reviewers each inspect the ACTUAL working-tree changes on one axis —
// correctness, persistence, integration — and list concrete findings. Every
// finding is then re-verified by an independent pass that must actively refute
// it to drop it; uncertain verdicts keep the finding so a real issue is never
// hidden. Confirmed findings flow into synthesize so the final answer names
// what still needs attention.
package hive

import (
	"context"
	"fmt"
	"strings"
)

// ReviewDimension is one axis the queen verifies. Focus is injected verbatim
// into the reviewer prompt so each reviewer stays on its single concern.
type ReviewDimension struct {
	Name  string
	Focus string
}

// DefaultReviewDimensions are the three axes checked after a hive run mutates
// the tree. Order is stable — reviewers are paired to dimensions by index.
func DefaultReviewDimensions() []ReviewDimension {
	return []ReviewDimension{
		{Name: "correctness", Focus: "does the code do what the task asked? logic bugs, wrong results, off-by-one, missed edge cases, broken or missing tests."},
		{Name: "persistence", Focus: "is state saved and loaded correctly? data integrity, migrations, ledgers, files written to the right path, no lost or double writes."},
		{Name: "integration", Focus: "does it fit the rest of the system? call sites updated, interfaces honored, no dangling references, the package still builds and wires together."},
	}
}

// Finding is one issue a reviewer raised plus the verifier's verdict. A finding
// with Confirmed=false was refuted on re-verification and is dropped from the
// synthesis (but retained in QueenResult.Findings for the record).
type Finding struct {
	Dimension string
	Claim     string
	Confirmed bool
	Verdict   string
}

// reviewAndVerify runs every dimension reviewer, then re-verifies each finding.
// Reviewers are paired to dimensions round-robin; the single Verifier (when set)
// re-checks every finding. Returns the full finding list (confirmed + refuted).
//
// The gate is advisory: a reviewer or verifier *model* error degrades to a
// flagged finding and the run continues, so a quality pass failing never
// discards the workers' completed (already-on-disk) changes. Only context
// cancellation aborts — that's the user pulling the plug.
func (q *Queen) reviewAndVerify(ctx context.Context, task string, plan []SubTask, results []Result) ([]Finding, error) {
	dims := q.ReviewDimensions
	if len(dims) == 0 {
		dims = DefaultReviewDimensions()
	}
	var findings []Finding
	for i, dim := range dims {
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		reviewer := q.Reviewers[i%len(q.Reviewers)]
		out, err := reviewer.Run(ctx, reviewPrompt(dim, task, plan, results))
		if err != nil {
			if ctx.Err() != nil {
				return findings, ctx.Err()
			}
			// non-fatal: surface the gap as a confirmed finding so it reaches
			// both the UI card and synthesis, then move on.
			f := Finding{Dimension: dim.Name, Claim: "review did not complete: " + err.Error(), Confirmed: true}
			q.Hooks.review(dim.Name, nil)
			q.Hooks.verify(f)
			findings = append(findings, f)
			continue
		}
		claims := parseFindings(out.FinalText)
		q.Hooks.review(dim.Name, claims)
		for _, claim := range claims {
			f := Finding{Dimension: dim.Name, Claim: claim, Confirmed: true}
			if q.Verifier != nil {
				vout, verr := q.Verifier.Run(ctx, verifyPrompt(dim, claim, task))
				switch {
				case verr != nil && ctx.Err() != nil:
					return findings, ctx.Err()
				case verr != nil:
					// can't re-verify → keep the finding (don't hide a possible
					// real issue), but mark it unverified.
					f.Verdict = "unverified: " + verr.Error()
				default:
					f.Confirmed, f.Verdict = parseVerdict(vout.FinalText)
				}
			}
			q.Hooks.verify(f)
			findings = append(findings, f)
		}
	}
	return findings, nil
}

// reviewPrompt builds the per-dimension reviewer instruction. The reviewer is
// read-only but has shell/read/grep — it inspects the live diff, not just the
// worker prose, so it catches what the reports omit.
func reviewPrompt(dim ReviewDimension, task string, plan []SubTask, results []Result) string {
	var b strings.Builder
	fmt.Fprintf(&b,
		"You are reviewing a hive run for ONE dimension: %s.\n"+
			"Focus: %s\n\n"+
			"Inspect the ACTUAL changes in the working tree (git diff, read, grep) "+
			"alongside the plan and worker reports below. Report only real, concrete "+
			"problems on THIS dimension.\n\n"+
			"Emit one line per problem, each prefixed `FINDING: `, citing file:line. "+
			"If you find nothing, output exactly `NONE`.\n\n"+
			"Original task: %s\n\n",
		dim.Name, dim.Focus, task)
	writePlanAndResults(&b, plan, results)
	return b.String()
}

// verifyPrompt asks an independent pass to confirm or refute one finding
// against the real code — the adversarial re-check.
func verifyPrompt(dim ReviewDimension, claim, task string) string {
	return fmt.Sprintf(
		"A %s reviewer raised this finding about the current working tree:\n\n%q\n\n"+
			"Independently verify it against the ACTUAL code (git diff, read, grep). "+
			"Decide whether it is a real problem.\n"+
			"Respond `CONFIRMED: <one-line reason>` if real, or "+
			"`REFUTED: <one-line reason>` if it is a false alarm.\n\n"+
			"Original task: %s",
		dim.Name, claim, task)
}

// renderFindings turns the verified finding list into the critique text fed to
// synthesize. Only confirmed findings are listed; refuted ones are counted.
func renderFindings(findings []Finding) string {
	if len(findings) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Verified review gate — correctness, persistence, integration:\n\n")
	var confirmed, refuted int
	for _, f := range findings {
		if !f.Confirmed {
			refuted++
			continue
		}
		confirmed++
		fmt.Fprintf(&b, "- [%s] %s", f.Dimension, f.Claim)
		if f.Verdict != "" {
			fmt.Fprintf(&b, " (verified: %s)", f.Verdict)
		}
		b.WriteByte('\n')
	}
	if confirmed == 0 {
		b.WriteString("- no confirmed issues after re-verification\n")
	}
	if refuted > 0 {
		fmt.Fprintf(&b, "\n(%d finding(s) refuted on re-verification, dropped.)\n", refuted)
	}
	return strings.TrimSpace(b.String())
}

// parseFindings pulls `FINDING:`-prefixed lines (case-insensitive) out of a
// reviewer's free-text output.
func parseFindings(text string) []string {
	var out []string
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if rest, ok := cutPrefixFold(ln, "FINDING:"); ok {
			if rest = strings.TrimSpace(rest); rest != "" {
				out = append(out, rest)
			}
		}
	}
	return out
}

// parseVerdict reads a verifier reply. A finding is dropped ONLY on an explicit
// REFUTED verdict; CONFIRMED and anything ambiguous keep it, so a borderline
// real issue surfaces rather than vanishing.
func parseVerdict(text string) (bool, string) {
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if rest, ok := cutPrefixFold(ln, "REFUTED"); ok {
			return false, trimVerdictReason(rest)
		}
		if rest, ok := cutPrefixFold(ln, "CONFIRMED"); ok {
			return true, trimVerdictReason(rest)
		}
	}
	return true, "" // ambiguous → keep
}

func trimVerdictReason(s string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), ":"))
}

// cutPrefixFold strips prefix from s (case-insensitive) and reports whether it
// matched.
func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return "", false
	}
	return s[len(prefix):], true
}
