package loop

import (
	"crypto/sha256"
	"encoding/hex"
)

// duplicateWriteTracker spots when the model writes identical content to the
// same path twice within one Run without intervening read. small models that
// lose context window can re-emit the same write — wastes turns and confuses
// downstream diffs. opt-in via Profile.Safety.WarnOnDuplicateWrites.
type duplicateWriteTracker struct {
	// lastWriteHash maps path → sha256(body) of the most recent write.
	lastWriteHash map[string]string
	// readSince tracks paths that have been read since the last write — clears
	// the dup-eligibility flag (the model may have observed a change).
	readSince map[string]bool
}

func newDuplicateWriteTracker() *duplicateWriteTracker {
	return &duplicateWriteTracker{
		lastWriteHash: map[string]string{},
		readSince:     map[string]bool{},
	}
}

// ObserveWrite records a write of body to path and returns true when this
// write duplicates the most recent write to the same path with no read in between.
func (t *duplicateWriteTracker) ObserveWrite(path, body string) bool {
	h := hashBody(body)
	dup := false
	if prev, ok := t.lastWriteHash[path]; ok && prev == h && !t.readSince[path] {
		dup = true
	}
	t.lastWriteHash[path] = h
	t.readSince[path] = false
	return dup
}

// ObserveRead clears the dup-eligibility for path so a subsequent identical
// write doesn't fire — the model may have re-confirmed file state and
// intentionally re-applied the same content.
func (t *duplicateWriteTracker) ObserveRead(path string) {
	t.readSince[path] = true
}

func hashBody(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}
