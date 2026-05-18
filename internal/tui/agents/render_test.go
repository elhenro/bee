package agents

import (
	"strings"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/bgreg"
)

func TestSectionHintDoneUnmergedSkipsFailedOnly(t *testing.T) {
	failedOnly := section{kind: secDoneUnmerged, rows: []row{
		{Status: bgreg.Status{State: bgreg.StateFailed}},
	}}
	if got := sectionHint(failedOnly); got != "" {
		t.Fatalf("failed-only section should suppress auto-merge hint, got %q", got)
	}

	mixed := section{kind: secDoneUnmerged, rows: []row{
		{Status: bgreg.Status{State: bgreg.StateFailed}},
		{Status: bgreg.Status{State: bgreg.StateDone}},
	}}
	if !strings.Contains(sectionHint(mixed), "auto-merging") {
		t.Fatalf("mixed section should still show auto-merge hint")
	}
}

func TestSectionHintNeedsInputDistinguishesConflict(t *testing.T) {
	awaiting := section{kind: secNeedsInput, rows: []row{
		{Status: bgreg.Status{State: bgreg.StateAwaiting}},
	}}
	if !strings.Contains(sectionHint(awaiting), "reply") {
		t.Fatalf("awaiting-only section should show reply hint, got %q", sectionHint(awaiting))
	}

	withConflict := section{kind: secNeedsInput, rows: []row{
		{Status: bgreg.Status{State: bgreg.StateAwaiting}},
		{Status: bgreg.Status{MergeState: bgreg.MergeStateConflict}},
	}}
	if !strings.Contains(sectionHint(withConflict), "conflict") {
		t.Fatalf("conflict-bearing section should show conflict hint, got %q", sectionHint(withConflict))
	}
}

func TestRenderRowUsesFinishedAtForCompletedDuration(t *testing.T) {
	start := time.Now().Add(-13 * time.Minute)
	finish := start.Add(2*time.Minute + 3*time.Second)

	out := renderRow(row{Status: bgreg.Status{
		State:      bgreg.StateDone,
		Task:       "done task",
		StartedAt:  start,
		FinishedAt: finish,
	}}, false, 80, Prefs{})

	if !strings.Contains(out, "2m03s") {
		t.Fatalf("renderRow should show finished duration, got %q", out)
	}
}
