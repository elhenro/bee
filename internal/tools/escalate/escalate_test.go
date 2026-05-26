package escalate

import (
	"context"
	"errors"
	"testing"
)

func TestEscalate_RunReturnsTypedError(t *testing.T) {
	tool := New()
	_, err := tool.Run(context.Background(), map[string]any{
		"reason":                "stuck on schema",
		"suggested_next_action": "ask user for example payload",
	})
	if err == nil {
		t.Fatalf("expected non-nil error")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *Error via errors.As, got %T", err)
	}
	if e.Reason != "stuck on schema" {
		t.Errorf("wrong reason: %q", e.Reason)
	}
	if e.NextAction != "ask user for example payload" {
		t.Errorf("wrong next: %q", e.NextAction)
	}
}

func TestEscalate_MissingReasonFallback(t *testing.T) {
	tool := New()
	_, err := tool.Run(context.Background(), map[string]any{})
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if e.Reason == "" {
		t.Errorf("expected fallback reason text, got empty")
	}
}

func TestEscalate_SpecAdvertisesRequiredReason(t *testing.T) {
	spec := New().Spec()
	if spec.Name != "escalate" {
		t.Errorf("wrong tool name: %q", spec.Name)
	}
	props, _ := spec.Schema["properties"].(map[string]any)
	if _, ok := props["reason"]; !ok {
		t.Errorf("schema missing reason property")
	}
}
