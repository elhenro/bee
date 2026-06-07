package tui

import (
	"strings"
	"testing"
)

// With handoffActive armed, a PickedMsg must route into the handoff path
// (onHandoffPicked) rather than a plain model swap. newTestModel has eng==nil,
// so the handoff path short-circuits to a "no engine" error — proof it diverted
// instead of calling SwitchProviderModel (which would no-op cleanly on nil eng).
func TestModel_Handoff_RoutesPickedMsg(t *testing.T) {
	m := newTestModel(t)
	m.handoffActive = true

	out, _ := m.Update(PickedMsg{Provider: "anthropic", Model: "claude-opus-4-8"})
	got := out.(Model)

	if got.handoffActive {
		t.Error("handoffActive must be consumed exactly once on the pick")
	}
	if got.state != StateError || !strings.Contains(got.lastErr, "no engine") {
		t.Fatalf("pick should route into the handoff path; got state=%v err=%q", got.state, got.lastErr)
	}
	if got.model == "claude-opus-4-8" {
		t.Error("handoff must defer the model swap to onHandoffReady, not switch at pick time")
	}
}

// A bail leaves StateError + lastErr set. handleSubmit's recovery clears
// lastErr, so the bail reason must be stashed into handoffStall first — else a
// following /handoff loses the highest-signal stall input.
func TestModel_Handoff_PreservesBailReason(t *testing.T) {
	m := newTestModel(t)
	m.state = StateError
	m.lastErr = "format strike — model wedged on malformed envelope"

	out, _ := m.handleSubmit() // empty input: recovers from error, no submit
	got := out.(Model)

	if got.lastErr != "" {
		t.Errorf("recovery should clear lastErr, got %q", got.lastErr)
	}
	if got.handoffStall != "format strike — model wedged on malformed envelope" {
		t.Errorf("bail reason must be preserved for /handoff, got %q", got.handoffStall)
	}
}

// Cancelling the picker mid-handoff disarms the sentinel so a later /model pick
// behaves normally.
func TestModel_Handoff_DismissDisarms(t *testing.T) {
	m := newTestModel(t)
	m.handoffActive = true

	out, _ := m.Update(PickerDismissedMsg{})
	got := out.(Model)
	if got.handoffActive {
		t.Error("picker dismissal must clear handoffActive")
	}
}
