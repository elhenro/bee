package session

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/elhenro/bee/internal/types"
)

func appendCorruptLine(t *testing.T, sid string) string {
	t.Helper()
	p, err := Path(sid)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString("{truncated-by-cra\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = f.Close()
	return p
}

// a crash-truncated line in the middle of a session file must not lose the
// rest of the session.
func TestRead_SkipsCorruptLine(t *testing.T) {
	withTempSessionsDir(t)
	sid := uuid.NewString()
	r, err := Open(sid)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()
	if err := r.Append(ctx, mkMsg("m1", "", types.RoleUser, "hello")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	appendCorruptLine(t, sid)
	r2, err := Open(sid)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := r2.Append(ctx, mkMsg("m2", "m1", types.RoleAssistant, "hi")); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	if err := r2.Close(); err != nil {
		t.Fatalf("Close 2: %v", err)
	}

	msgs, err := Read(sid)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(msgs) != 2 || msgs[0].ID != "m1" || msgs[1].ID != "m2" {
		t.Fatalf("expected m1+m2 around corrupt line, got: %+v", msgs)
	}
}

// one corrupt session file must not abort the whole listing; healthy sessions
// stay visible and the corrupt-first-line one falls back gracefully.
func TestList_SurvivesCorruptFile(t *testing.T) {
	dir := withTempSessionsDir(t)

	sid := uuid.NewString()
	r, err := Open(sid)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := r.Append(context.Background(), mkMsg("m1", "", types.RoleUser, "hello")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// a session whose only content is a corrupt line
	bad := dir + "/" + uuid.NewString() + ".jsonl"
	if err := os.WriteFile(bad, []byte("{garbage\n"), 0o644); err != nil {
		t.Fatalf("write bad: %v", err)
	}

	sessions, err := List()
	if err != nil {
		t.Fatalf("List must not fail on a corrupt file: %v", err)
	}
	found := false
	for _, s := range sessions {
		if s.ID == sid {
			found = true
		}
	}
	if !found {
		t.Fatalf("healthy session %s missing from listing: %+v", sid, sessions)
	}
}
