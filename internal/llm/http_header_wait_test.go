package llm

import (
	"net/http"
	"testing"
	"time"
)

// servers that flush headers only with the first token must get the stall
// window, not a short fixed cap.
func TestStreamingClientHeaderWaitFollowsStallWindow(t *testing.T) {
	hdr := func() time.Duration {
		return newStreamingClient().Transport.(*http.Transport).ResponseHeaderTimeout
	}
	t.Setenv("BEE_STREAM_STALL_SECONDS", "")
	if got := hdr(); got != streamStallTimeout {
		t.Fatalf("default header wait = %v, want %v", got, streamStallTimeout)
	}
	t.Setenv("BEE_STREAM_STALL_SECONDS", "300")
	if got := hdr(); got != 300*time.Second {
		t.Fatalf("env header wait = %v, want 5m", got)
	}
	t.Setenv("BEE_STREAM_STALL_SECONDS", "0")
	if got := hdr(); got != 0 {
		t.Fatalf("disabled watchdog header wait = %v, want 0 (none)", got)
	}
}
