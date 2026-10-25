package loop

// duplicateWriteTracker spots when the model writes identical content to the
// same path twice within one Run without intervening read. small models that
// lose context window can re-emit the same write — wastes turns and confuses
// downstream diffs. opt-in via Profile.Safety.WarnOnDuplicateWrites.
type duplicateWriteTracker struct {
	// lastWrite maps path → hash of the most recent write's body. a read of the
	// path drops its entry: the model may have observed a change and
	// intentionally re-applied the same content.
	lastWrite map[string]string
}

func newDuplicateWriteTracker() *duplicateWriteTracker {
	return &duplicateWriteTracker{lastWrite: map[string]string{}}
}

// ObserveWrite records a write of body to path and returns true when this
// write duplicates the most recent write to the same path with no read in between.
func (t *duplicateWriteTracker) ObserveWrite(path, body string) bool {
	h := hash8(body)
	dup := t.lastWrite[path] == h
	t.lastWrite[path] = h
	return dup
}

// ObserveRead clears the dup-eligibility for path so a subsequent identical
// write doesn't fire.
func (t *duplicateWriteTracker) ObserveRead(path string) {
	delete(t.lastWrite, path)
}
