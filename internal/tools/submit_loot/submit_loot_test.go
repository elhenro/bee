package submit_loot

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunEmitsSentinelLine(t *testing.T) {
	var buf strings.Builder
	tool := New(&buf)
	res, err := tool.Run(context.Background(), map[string]any{"flag": "FLAG{abc123}"})
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if res.IsError {
		t.Fatalf("Run returned IsError for a valid flag: %q", res.Content)
	}
	line := strings.TrimSpace(buf.String())
	var sentinel struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(line), &sentinel); err != nil {
		t.Fatalf("sentinel not valid JSON %q: %v", line, err)
	}
	if sentinel.Type != "loot" {
		t.Fatalf("sentinel type = %q, want loot", sentinel.Type)
	}
	if sentinel.Content != "FLAG{abc123}" {
		t.Fatalf("sentinel content = %q, want the submitted flag", sentinel.Content)
	}
}

func TestRunMissingFlagIsError(t *testing.T) {
	var buf strings.Builder
	tool := New(&buf)
	res, err := tool.Run(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if !res.IsError {
		t.Fatal("missing flag should yield an error result")
	}
	if buf.Len() != 0 {
		t.Fatalf("no sentinel should be emitted on error, got %q", buf.String())
	}
}

func TestSpecName(t *testing.T) {
	if New(nil).Spec().Name != "submit_loot" {
		t.Fatalf("spec name = %q, want submit_loot", New(nil).Spec().Name)
	}
}
