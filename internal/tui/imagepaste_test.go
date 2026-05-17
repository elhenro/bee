package tui

import "testing"

// TestReadClipboardImage_NoImage is a smoke test: in CI the clipboard backend
// is usually unavailable (init returns an error); locally it may be available
// but typically empty. Either way the function must return — never panic —
// and never succeed with zero bytes.
func TestReadClipboardImage_NoImage(t *testing.T) {
	b, err := ReadClipboardImage()
	if err == nil {
		// local run with an image actually staged is fine
		if len(b) == 0 {
			t.Fatal("nil error but no bytes — contract violated")
		}
		t.Logf("clipboard had %d image bytes (local run)", len(b))
		return
	}
	// CI / headless: must surface a clear error, not panic
	if err.Error() == "" {
		t.Fatal("error must have a non-empty message")
	}
}
