package goal

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ReplyFingerprint returns a stable digest of a reply's semantic content:
// line endings are normalized and outer whitespace trimmed, but everything
// inside is hashed verbatim so any inner change alters the digest. An empty
// or whitespace-only reply yields "" — there is no content to fingerprint.
func ReplyFingerprint(reply string) string {
	norm := NormalizeReply(reply)
	if norm == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}

// NormalizeReply canonicalizes line endings (CRLF and lone CR become LF) and
// trims outer whitespace only, leaving inner spacing and content untouched.
func NormalizeReply(reply string) string {
	if strings.ContainsRune(reply, '\r') {
		reply = strings.ReplaceAll(reply, "\r\n", "\n")
		reply = strings.ReplaceAll(reply, "\r", "\n")
	}
	return strings.TrimSpace(reply)
}

// FingerprintTracker remembers the last non-empty reply fingerprint for each
// goal thread (a compacted-goal identity) so a loop can flag an unchanged
// reply as no progress. The zero value is ready to use.
type FingerprintTracker struct {
	last map[string]string
}

// Observe records reply under thread and reports whether it repeats the
// thread's previous reply. A first non-empty reply reports false; an empty or
// whitespace-only reply reports false and leaves the stored fingerprint
// untouched. A repeat of the stored fingerprint returns true.
func (t *FingerprintTracker) Observe(thread, reply string) bool {
	fp := ReplyFingerprint(reply)
	if fp == "" {
		return false
	}
	if t.last == nil {
		t.last = make(map[string]string)
	}
	prev, seen := t.last[thread]
	t.last[thread] = fp
	return seen && prev == fp
}

// Last returns the stored fingerprint for thread, or "" when none is recorded.
func (t *FingerprintTracker) Last(thread string) string {
	return t.last[thread]
}