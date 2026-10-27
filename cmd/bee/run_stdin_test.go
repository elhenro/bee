package main

import (
	"strings"
	"testing"
)

func TestResolveUserMessageStdinTruncationIsReported(t *testing.T) {
	var got string
	out := captureStderr(t, func() {
		got, _ = resolveUserMessage(nil, strings.NewReader(strings.Repeat("x", maxStdinBytes+10)))
	})
	if len(got) != maxStdinBytes {
		t.Errorf("message length = %d, want %d", len(got), maxStdinBytes)
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("truncation not reported on stderr: %q", out)
	}
}

func TestResolveUserMessageStdinExactFitIsSilent(t *testing.T) {
	out := captureStderr(t, func() {
		_, _ = resolveUserMessage(nil, strings.NewReader(strings.Repeat("x", maxStdinBytes)))
	})
	if out != "" {
		t.Errorf("message that fits was reported as truncated: %q", out)
	}
}
