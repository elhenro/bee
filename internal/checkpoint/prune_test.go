package checkpoint

import (
	"path/filepath"
	"testing"
)

// read-only turns leave the tree unchanged; Prune must still keep the newest
// turn's snapshot, not an arbitrary one that shares an old commit.
func TestPruneKeepsNewestUnchangedSnapshot(t *testing.T) {
	s, root := newStore(t)
	write(t, filepath.Join(root, "a.txt"), "one")
	for _, id := range []string{"m1", "m2", "m3"} {
		if _, err := s.Snapshot(id, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Prune(1); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.SnapshotFor("m3"); !ok {
		t.Fatal("newest snapshot pruned")
	}
	if _, ok := s.SnapshotFor("m1"); ok {
		t.Fatal("oldest snapshot survived Prune(1)")
	}
}
