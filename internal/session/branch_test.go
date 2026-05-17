package session

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/elhenro/bee/internal/types"
)

// shape:
//   m1 (root)
//    └── m2
//         ├── m3 ── m4
//         └── m5  (branch)
func sampleBranchedMessages() []types.Message {
	return []types.Message{
		mkMsg("m1", "", types.RoleUser, "root"),
		mkMsg("m2", "m1", types.RoleAssistant, "a"),
		mkMsg("m3", "m2", types.RoleUser, "main-branch"),
		mkMsg("m4", "m3", types.RoleAssistant, "main-leaf"),
		mkMsg("m5", "m2", types.RoleUser, "alt-branch-leaf"),
	}
}

func TestBuildTree_Shape(t *testing.T) {
	msgs := sampleBranchedMessages()
	root := BuildTree(msgs)
	if root == nil {
		t.Fatal("BuildTree returned nil")
	}
	if root.Msg.ID != "m1" {
		t.Fatalf("root id = %q want m1", root.Msg.ID)
	}
	if len(root.Children) != 1 || root.Children[0].Msg.ID != "m2" {
		t.Fatalf("root children = %+v", root.Children)
	}
	m2 := root.Children[0]
	if len(m2.Children) != 2 {
		t.Fatalf("m2 children count = %d want 2", len(m2.Children))
	}
	gotIDs := map[string]bool{}
	for _, c := range m2.Children {
		gotIDs[c.Msg.ID] = true
	}
	if !gotIDs["m3"] || !gotIDs["m5"] {
		t.Fatalf("m2 children = %v", gotIDs)
	}
	// m3 has m4
	var m3 *types.MessageNode
	for _, c := range m2.Children {
		if c.Msg.ID == "m3" {
			m3 = c
		}
	}
	if m3 == nil || len(m3.Children) != 1 || m3.Children[0].Msg.ID != "m4" {
		t.Fatalf("m3 subtree wrong: %+v", m3)
	}
}

func TestBuildTree_Empty(t *testing.T) {
	if BuildTree(nil) != nil {
		t.Fatal("expected nil for empty input")
	}
}

func TestLinearPath_MainLeaf(t *testing.T) {
	root := BuildTree(sampleBranchedMessages())
	path := LinearPath(root, "m4")
	want := []string{"m1", "m2", "m3", "m4"}
	if len(path) != len(want) {
		t.Fatalf("path len = %d want %d (path=%v)", len(path), len(want), pathIDs(path))
	}
	for i, id := range want {
		if path[i].Msg.ID != id {
			t.Errorf("path[%d] = %q want %q", i, path[i].Msg.ID, id)
		}
	}
}

func TestLinearPath_AltLeaf(t *testing.T) {
	root := BuildTree(sampleBranchedMessages())
	path := LinearPath(root, "m5")
	want := []string{"m1", "m2", "m5"}
	if len(path) != len(want) {
		t.Fatalf("path = %v want %v", pathIDs(path), want)
	}
	for i, id := range want {
		if path[i].Msg.ID != id {
			t.Errorf("path[%d] = %q want %q", i, path[i].Msg.ID, id)
		}
	}
}

func TestLinearPath_RootOnly(t *testing.T) {
	root := BuildTree(sampleBranchedMessages())
	path := LinearPath(root, "m1")
	if len(path) != 1 || path[0].Msg.ID != "m1" {
		t.Fatalf("root path = %v", pathIDs(path))
	}
}

func TestLinearPath_Missing(t *testing.T) {
	root := BuildTree(sampleBranchedMessages())
	if path := LinearPath(root, "nope"); path != nil {
		t.Fatalf("missing leaf should return nil, got %v", pathIDs(path))
	}
}

func pathIDs(p []*types.MessageNode) []string {
	out := make([]string, len(p))
	for i, n := range p {
		out[i] = n.Msg.ID
	}
	return out
}

// seedSession appends the given messages into a fresh session and returns its id.
func seedSession(t *testing.T, msgs []types.Message) string {
	t.Helper()
	sid := uuid.NewString()
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
	return sid
}

func TestFork_PartialUpToMsgID(t *testing.T) {
	withTempSessionsDir(t)
	src := seedSession(t, []types.Message{
		mkMsg("m1", "", types.RoleUser, "1"),
		mkMsg("m2", "m1", types.RoleAssistant, "2"),
		mkMsg("m3", "m2", types.RoleUser, "3"),
		mkMsg("m4", "m3", types.RoleAssistant, "4"),
		mkMsg("m5", "m4", types.RoleUser, "5"),
	})

	r, err := Fork(src, "m3")
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	newID := r.ID()
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if newID == src {
		t.Fatalf("fork must use a new id, got %q", newID)
	}

	got, err := Read(newID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := []string{"m1", "m2", "m3"}
	if len(got) != len(want) {
		t.Fatalf("fork len = %d want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("fork[%d] = %q want %q", i, got[i].ID, id)
		}
	}
}

func TestFork_NotFound(t *testing.T) {
	withTempSessionsDir(t)
	src := seedSession(t, []types.Message{
		mkMsg("m1", "", types.RoleUser, "1"),
		mkMsg("m2", "m1", types.RoleAssistant, "2"),
	})

	if _, err := Fork(src, "nonexistent"); err == nil {
		t.Fatal("expected error for unknown message id")
	}
}

func TestClone_FullCopy(t *testing.T) {
	withTempSessionsDir(t)
	src := seedSession(t, []types.Message{
		mkMsg("m1", "", types.RoleUser, "1"),
		mkMsg("m2", "m1", types.RoleAssistant, "2"),
		mkMsg("m3", "m2", types.RoleUser, "3"),
		mkMsg("m4", "m3", types.RoleAssistant, "4"),
	})

	r, err := Clone(src)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	newID := r.ID()
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if newID == src {
		t.Fatal("clone id must differ from source")
	}

	got, err := Read(newID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := []string{"m1", "m2", "m3", "m4"}
	if len(got) != len(want) {
		t.Fatalf("clone len = %d want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("clone[%d] = %q want %q", i, got[i].ID, id)
		}
	}
}
