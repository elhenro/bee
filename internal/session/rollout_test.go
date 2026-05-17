package session

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/elhenro/bee/internal/types"
)

func withTempSessionsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("BEE_SESSIONS_DIR", dir)
	return dir
}

func mkMsg(id, parent string, role types.Role, text string) types.Message {
	return types.Message{
		ID:       id,
		ParentID: parent,
		Role:     role,
		Content:  []types.ContentBlock{{Type: types.BlockText, Text: text}},
		Time:     time.Now().UTC(),
	}
}

func TestRollout_AppendReadList(t *testing.T) {
	withTempSessionsDir(t)

	sid := uuid.NewString()
	r, err := Open(sid)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	msgs := []types.Message{
		mkMsg("m1", "", types.RoleUser, "hello"),
		mkMsg("m2", "m1", types.RoleAssistant, "hi"),
		mkMsg("m3", "m2", types.RoleUser, "how are you"),
		mkMsg("m4", "m3", types.RoleAssistant, "good"),
		mkMsg("m5", "m4", types.RoleUser, "bye"),
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

	// file should exist
	p, err := Path(sid)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("stat %s: %v", p, err)
	}

	got, err := Read(sid)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != len(msgs) {
		t.Fatalf("Read len: got %d want %d", len(got), len(msgs))
	}
	for i := range msgs {
		if got[i].ID != msgs[i].ID || got[i].ParentID != msgs[i].ParentID {
			t.Errorf("msg %d mismatch: got %+v want %+v", i, got[i], msgs[i])
		}
		if len(got[i].Content) != 1 || got[i].Content[0].Text != msgs[i].Content[0].Text {
			t.Errorf("msg %d content mismatch", i)
		}
	}

	sessions, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, s := range sessions {
		if s.ID == sid {
			found = true
			if s.Created.IsZero() {
				t.Errorf("session %s has zero Created", sid)
			}
		}
	}
	if !found {
		t.Errorf("session %s not in List: %+v", sid, sessions)
	}
}

func TestRollout_AppendAfterClose(t *testing.T) {
	withTempSessionsDir(t)
	r, err := Open(uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Append(context.Background(), mkMsg("x", "", types.RoleUser, "x")); err == nil {
		t.Fatal("expected error appending to closed rollout")
	}
}

func TestRollout_ReopenAppends(t *testing.T) {
	withTempSessionsDir(t)
	sid := uuid.NewString()

	r1, err := Open(sid)
	if err != nil {
		t.Fatal(err)
	}
	if err := r1.Append(context.Background(), mkMsg("a", "", types.RoleUser, "1")); err != nil {
		t.Fatal(err)
	}
	r1.Close()

	r2, err := Open(sid)
	if err != nil {
		t.Fatal(err)
	}
	if err := r2.Append(context.Background(), mkMsg("b", "a", types.RoleAssistant, "2")); err != nil {
		t.Fatal(err)
	}
	r2.Close()

	msgs, err := Read(sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].ID != "a" || msgs[1].ID != "b" {
		t.Fatalf("reopen: got %+v", msgs)
	}
}

func TestList_EmptyDir(t *testing.T) {
	withTempSessionsDir(t)
	got, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %+v", got)
	}
}
