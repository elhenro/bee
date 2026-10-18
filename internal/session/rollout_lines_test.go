package session

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
	"github.com/google/uuid"
)

// one message past the line cap (an inlined screenshot) is skipped; the rest
// of the session still loads.
func TestRead_SkipsOverlongLine(t *testing.T) {
	withTempSessionsDir(t)
	sid := uuid.NewString()
	r, err := Open(sid)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = r.Append(ctx, mkMsg("m1", "", types.RoleUser, "before"))
	_ = r.Append(ctx, mkMsg("m2", "m1", types.RoleAssistant, strings.Repeat("x", maxLineBytes+1024)))
	_ = r.Append(ctx, mkMsg("m3", "m2", types.RoleUser, "after"))
	_ = r.Close()

	msgs, err := Read(sid)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(msgs) != 2 || msgs[0].ID != "m1" || msgs[1].ID != "m3" {
		t.Fatalf("got %d messages, want m1 and m3 around the skipped one", len(msgs))
	}
}

// a crash that left a fragment without its newline must not swallow the next
// session's first append.
func TestOpen_TerminatesTornLastLine(t *testing.T) {
	withTempSessionsDir(t)
	sid := uuid.NewString()
	r, err := Open(sid)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Append(context.Background(), mkMsg("m1", "", types.RoleUser, "hello"))
	_ = r.Close()

	p, _ := Path(sid)
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString(`{"id":"torn`)
	_ = f.Close()

	r2, err := Open(sid)
	if err != nil {
		t.Fatal(err)
	}
	_ = r2.Append(context.Background(), mkMsg("m2", "m1", types.RoleAssistant, "hi"))
	_ = r2.Close()

	msgs, err := Read(sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[1].ID != "m2" {
		t.Fatalf("got %d messages, want m1 and m2", len(msgs))
	}
}
