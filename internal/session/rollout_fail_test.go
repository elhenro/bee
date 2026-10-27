package session

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/elhenro/bee/internal/types"
	"github.com/google/uuid"
)

// failingFile wraps a real file: the first Write lands half its bytes and fails
// (a full disk), later ones pass through; Sync can be made to fail.
type failingFile struct {
	*os.File
	failFirst bool
	syncErr   error
}

func (f *failingFile) Write(b []byte) (int, error) {
	if f.failFirst {
		f.failFirst = false
		n, _ := f.File.Write(b[:len(b)/2])
		return n, errors.New("disk full")
	}
	return f.File.Write(b)
}

func (f *failingFile) Sync() error {
	if f.syncErr != nil {
		return f.syncErr
	}
	return f.File.Sync()
}

func openFailing(t *testing.T, sid string, ff func(*os.File) *failingFile) *Rollout {
	t.Helper()
	r, err := Open(sid)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	file := r.f.(*os.File)
	r.f = ff(file)
	return r
}

// a failed write leaves a fragment; the next Append must end it instead of
// gluing onto it, or both messages are lost.
func TestAppendTerminatesFragmentOfFailedWrite(t *testing.T) {
	withTempSessionsDir(t)
	sid := uuid.NewString()
	r := openFailing(t, sid, func(f *os.File) *failingFile { return &failingFile{File: f, failFirst: true} })
	ctx := context.Background()

	if err := r.Append(ctx, mkMsg("m1", "", types.RoleUser, "lost to the disk")); err == nil {
		t.Fatal("first Append should fail")
	}
	if err := r.Append(ctx, mkMsg("m2", "", types.RoleUser, "survives")); err != nil {
		t.Fatalf("second Append: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	msgs, err := Read(sid)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(msgs) != 1 || msgs[0].ID != "m2" {
		t.Fatalf("read %d messages (%v), want only m2", len(msgs), msgs)
	}
}

// the final fsync is the durability guarantee, so Close must report its failure.
func TestCloseReturnsSyncError(t *testing.T) {
	withTempSessionsDir(t)
	want := errors.New("fsync failed")
	r := openFailing(t, uuid.NewString(), func(f *os.File) *failingFile { return &failingFile{File: f, syncErr: want} })
	if err := r.Append(context.Background(), mkMsg("m1", "", types.RoleUser, "x")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := r.Close(); !errors.Is(err, want) {
		t.Fatalf("Close = %v, want the sync error", err)
	}
}
