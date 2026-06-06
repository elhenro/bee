package session

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/elhenro/bee/internal/types"
)

func ckpt(id, preserveFrom, summary string) types.Message {
	m := mkMsg(id, "", types.RoleUser, summary)
	m.Checkpoint = &types.Checkpoint{PreserveFrom: preserveFrom}
	return m
}

func ids(msgs []types.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.ID
	}
	return out
}

func writeAll(t *testing.T, sid string, msgs []types.Message) {
	t.Helper()
	r, err := Open(sid)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()
	for _, m := range msgs {
		if err := r.Append(ctx, m); err != nil {
			t.Fatalf("Append %s: %v", m.ID, err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestReadResume_NoCheckpoint_ReturnsRaw(t *testing.T) {
	withTempSessionsDir(t)
	sid := uuid.NewString()
	writeAll(t, sid, []types.Message{
		mkMsg("m1", "", types.RoleUser, "a"),
		mkMsg("m2", "m1", types.RoleAssistant, "b"),
	})
	got, err := ReadResume(sid)
	if err != nil {
		t.Fatalf("ReadResume: %v", err)
	}
	if want := []string{"m1", "m2"}; !equal(ids(got), want) {
		t.Fatalf("got %v want %v", ids(got), want)
	}
}

func TestReadResume_CollapsesAtCheckpoint(t *testing.T) {
	withTempSessionsDir(t)
	sid := uuid.NewString()
	// m1..m6 old, checkpoint preserves from m5, then m7,m8 appended after.
	writeAll(t, sid, []types.Message{
		mkMsg("m1", "", types.RoleUser, "1"),
		mkMsg("m2", "m1", types.RoleAssistant, "2"),
		mkMsg("m3", "m2", types.RoleUser, "3"),
		mkMsg("m4", "m3", types.RoleAssistant, "4"),
		mkMsg("m5", "m4", types.RoleUser, "5"),
		mkMsg("m6", "m5", types.RoleAssistant, "6"),
		ckpt("cp1", "m5", "[compacted history]\nsummary"),
		mkMsg("m7", "m6", types.RoleUser, "7"),
		mkMsg("m8", "m7", types.RoleAssistant, "8"),
	})
	got, err := ReadResume(sid)
	if err != nil {
		t.Fatalf("ReadResume: %v", err)
	}
	// summary first (marker stripped), then preserved tail m5,m6 + later m7,m8.
	if want := []string{"cp1", "m5", "m6", "m7", "m8"}; !equal(ids(got), want) {
		t.Fatalf("got %v want %v", ids(got), want)
	}
	if got[0].Checkpoint != nil {
		t.Fatalf("summary should have marker stripped")
	}
	if got[0].Content[0].Text != "[compacted history]\nsummary" {
		t.Fatalf("summary text lost: %q", got[0].Content[0].Text)
	}
}

func TestReadResume_UsesLastCheckpoint(t *testing.T) {
	withTempSessionsDir(t)
	sid := uuid.NewString()
	writeAll(t, sid, []types.Message{
		mkMsg("m1", "", types.RoleUser, "1"),
		mkMsg("m2", "m1", types.RoleAssistant, "2"),
		ckpt("cp1", "m2", "first summary"),
		mkMsg("m3", "m2", types.RoleUser, "3"),
		mkMsg("m4", "m3", types.RoleAssistant, "4"),
		ckpt("cp2", "m4", "second summary"),
		mkMsg("m5", "m4", types.RoleUser, "5"),
	})
	got, err := ReadResume(sid)
	if err != nil {
		t.Fatalf("ReadResume: %v", err)
	}
	// last checkpoint wins; cp1 marker dropped, m2 folded into cp2's summary.
	if want := []string{"cp2", "m4", "m5"}; !equal(ids(got), want) {
		t.Fatalf("got %v want %v", ids(got), want)
	}
}

func TestReadResume_MissingBoundary_FallsBackToRaw(t *testing.T) {
	withTempSessionsDir(t)
	sid := uuid.NewString()
	raw := []types.Message{
		mkMsg("m1", "", types.RoleUser, "1"),
		mkMsg("m2", "m1", types.RoleAssistant, "2"),
		ckpt("cp1", "ghost", "summary"),
		mkMsg("m3", "m2", types.RoleUser, "3"),
	}
	writeAll(t, sid, raw)
	got, err := ReadResume(sid)
	if err != nil {
		t.Fatalf("ReadResume: %v", err)
	}
	if want := []string{"m1", "m2", "cp1", "m3"}; !equal(ids(got), want) {
		t.Fatalf("got %v want %v", ids(got), want)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
