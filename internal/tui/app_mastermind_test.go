package tui

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

func TestHiveTaskFromContent_JoinsTextBlocks(t *testing.T) {
	got := hiveTaskFromContent([]types.ContentBlock{
		{Type: types.BlockText, Text: "first part"},
		{Type: types.BlockText, Text: "  "},
		{Type: types.BlockText, Text: "second part"},
	})
	if !strings.Contains(got, "first part") || !strings.Contains(got, "second part") {
		t.Errorf("both text blocks should survive: %q", got)
	}
	if strings.Contains(got, "image") {
		t.Errorf("no image note expected without images: %q", got)
	}
}

func TestHiveTaskFromContent_ImageNote(t *testing.T) {
	got := hiveTaskFromContent([]types.ContentBlock{
		{Type: types.BlockText, Text: "look at these"},
		{Type: types.BlockImage, MediaType: "image/png"},
		{Type: types.BlockImage, MediaType: "image/jpeg"},
	})
	if !strings.Contains(got, "look at these") {
		t.Errorf("text block should survive: %q", got)
	}
	if !strings.Contains(got, "2 image(s)") {
		t.Errorf("expected image count note: %q", got)
	}
}

func TestFormatReview_Clean(t *testing.T) {
	got := formatReview("correctness", nil)
	if !strings.Contains(got, "review — correctness") {
		t.Errorf("missing dimension header: %q", got)
	}
	if !strings.Contains(got, "no findings") {
		t.Errorf("clean card should say no findings: %q", got)
	}
}

func TestFormatReview_ConfirmedAndRefuted(t *testing.T) {
	got := formatReview("persistence", []hive.Finding{
		{Dimension: "persistence", Claim: "ledger not flushed", Confirmed: true, Verdict: "real, foo.go:12"},
		{Dimension: "persistence", Claim: "maybe a race", Confirmed: false, Verdict: "false alarm"},
	})
	if !strings.Contains(got, "✓ ledger not flushed") {
		t.Errorf("confirmed finding mismark: %q", got)
	}
	if !strings.Contains(got, "(real, foo.go:12)") {
		t.Errorf("missing verdict: %q", got)
	}
	if !strings.Contains(got, "✗ maybe a race") || !strings.Contains(got, "dropped") {
		t.Errorf("refuted finding should be marked dropped: %q", got)
	}
}

// TestWireHivePumps_BindsAllFour is a regression guard for the queen
// streaming fix: in queen mode the parent engine never runs, so the planner's
// first call (decompose) and every worker / reviewer / verifier / synthesize
// turn live on sub-engines. SpawnWorker leaves ThinkCh / StreamCh /
// ProgressCh nil and rewires only LiveMsgCh at the mastermind level; before
// the fix the spawn closure forgot the other three, so reasoning + text
// deltas hit nil-channel selects in turn_stream.go and were dropped — the
// user saw a spinner through the entire planning phase. This test calls the
// real wiring helper so dropping any one of the four assignments fails CI.
func TestWireHivePumps_BindsAllFour(t *testing.T) {
	liveCh := make(chan types.Message, 1)
	streamCh := make(chan string, 1)
	thinkCh := make(chan string, 1)
	progressCh := make(chan int, 1)
	eng := &loop.Engine{}
	wireHivePumps(eng, liveCh, streamCh, thinkCh, progressCh)
	if eng.LiveMsgCh != liveCh {
		t.Errorf("LiveMsgCh not bound: got %p, want %p", eng.LiveMsgCh, liveCh)
	}
	if eng.StreamCh != streamCh {
		t.Errorf("StreamCh not bound: got %p, want %p", eng.StreamCh, streamCh)
	}
	if eng.ThinkCh != thinkCh {
		t.Errorf("ThinkCh not bound: got %p, want %p", eng.ThinkCh, thinkCh)
	}
	if eng.ProgressCh != progressCh {
		t.Errorf("ProgressCh not bound: got %p, want %p", eng.ProgressCh, progressCh)
	}
	// nil engine must be a no-op, not a panic.
	wireHivePumps(nil, liveCh, streamCh, thinkCh, progressCh)
}
