package zzz

import (
	"context"
	"strings"
	"testing"
)

// DONE after the gate dropped the change means nothing landed: not completed
func TestDrive_DONEWithDroppedChangeIsNotCompleted(t *testing.T) {
	run, repo := setupDriveTest(t)
	r := &stubRunner{dir: repo, turns: []stubTurn{
		{text: "added a.txt", write: map[string]string{"a.txt": "work"}},
		{text: "DONE: fixed it", write: map[string]string{"a.txt": "work2"}},
	}}
	ui := &stubUI{steer: make(chan Steer)}
	cfg := Config{Objective: "x", MaxIterations: 5, Verify: []string{"false"}, MaxHops: 1}
	if err := Drive(context.Background(), nil, r, cfg, run, ui); err != nil {
		t.Fatal(err)
	}
	if run.Status == StatusCompleted || len(run.Commits) != 0 {
		t.Fatalf("status=%s cause=%q commits=%d, want not completed without commits", run.Status, run.StopCause, len(run.Commits))
	}
}

func TestDrive_StopsRecordEndTimeAndSummary(t *testing.T) {
	t.Run("graceful stop", func(t *testing.T) {
		run, repo := setupDriveTest(t)
		stop := make(chan struct{})
		close(stop)
		ui := &stubUI{steer: make(chan Steer)}
		_ = Drive(context.Background(), stop, &stubRunner{dir: repo}, Config{Objective: "x", MaxIterations: 3}, run, ui)
		assertEnded(t, run, ui)
	})
	t.Run("operator abort", func(t *testing.T) {
		run, repo := setupDriveTest(t)
		ui := &stubUI{steer: make(chan Steer, 1)}
		ui.steer <- Steer{Kind: SteerAbort}
		_ = Drive(context.Background(), nil, &stubRunner{dir: repo}, Config{Objective: "x", MaxIterations: 3}, run, ui)
		assertEnded(t, run, ui)
	})
	t.Run("operator stop", func(t *testing.T) {
		run, repo := setupDriveTest(t)
		ui := &stubUI{steer: make(chan Steer, 1)}
		ui.steer <- Steer{Kind: SteerStop}
		_ = Drive(context.Background(), nil, &stubRunner{dir: repo}, Config{Objective: "x", MaxIterations: 3}, run, ui)
		assertEnded(t, run, ui)
	})
}

func assertEnded(t *testing.T, run *Run, ui *stubUI) {
	t.Helper()
	if run.Status != StatusAborted || run.EndedAt.IsZero() {
		t.Fatalf("status=%s ended zero=%v, want aborted with an end time", run.Status, run.EndedAt.IsZero())
	}
	saved, err := LoadMeta(run.ID)
	if err != nil || saved.EndedAt.IsZero() || saved.Status != StatusAborted {
		t.Fatalf("persisted meta=%+v err=%v", saved, err)
	}
	if ui.summary == nil {
		t.Fatal("summary not rendered")
	}
}

// operator notes ride on the next prompt; blank ones and a closed steer
// channel are harmless.
func TestDrive_SteerNotesReachNextPrompt(t *testing.T) {
	run, repo := setupDriveTest(t)
	r := &stubRunner{dir: repo, turns: []stubTurn{{text: "DONE: nothing to do"}}}
	ui := &stubUI{steer: make(chan Steer, 3)}
	ui.steer <- Steer{Kind: SteerNote, Text: "  prefer table-driven tests  "}
	ui.steer <- Steer{Kind: SteerNote, Text: "   "}
	close(ui.steer)
	if err := Drive(context.Background(), nil, r, Config{Objective: "x", MaxIterations: 2}, run, ui); err != nil {
		t.Fatal(err)
	}
	if run.Status != StatusCompleted || len(r.prompts) != 1 {
		t.Fatalf("status=%s cause=%q prompts=%d", run.Status, run.StopCause, len(r.prompts))
	}
	// first iteration: no prior notes, so the nudge block is the prompt's tail
	if _, after, _ := strings.Cut(r.prompts[0], "(live input):\n"); after != "- prefer table-driven tests\n" {
		t.Fatalf("nudge block = %q, want the one trimmed nudge", after)
	}
}
