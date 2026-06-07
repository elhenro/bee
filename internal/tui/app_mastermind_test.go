package tui

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/hive"
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
