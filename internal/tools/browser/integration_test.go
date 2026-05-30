package browser

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIntegration_OpenSnapshotClickConsole(t *testing.T) {
	if os.Getenv("BEE_BROWSER_TEST") != "1" {
		t.Skip("set BEE_BROWSER_TEST=1 to run (needs Chrome/Chromium)")
	}
	path, err := DetectChrome("")
	if err != nil {
		t.Skipf("no chrome: %v", err)
	}
	sess := NewSession(path, true) // headless for CI
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// single-line data: URL; multiline ones choke Chrome's Navigate
	const page = `data:text/html,<html><body><button onclick="console.log('clicked!');document.title='done'">Press</button><script>console.log('loaded')</script></body></html>`

	ts := New(Options{ChromePath: path, Headless: true})
	var open, snap, console, click Tool
	for _, x := range ts {
		switch x.Spec().Name {
		case "browser_open":
			open = x.(Tool)
		case "browser_snapshot":
			snap = x.(Tool)
		case "browser_console":
			console = x.(Tool)
		case "browser_click":
			click = x.(Tool)
		}
	}
	// share one session across these tool instances
	open.sess, snap.sess, console.sess, click.sess = sess, sess, sess, sess

	if r := open.Run2(ctx, map[string]any{"url": page}); r.IsError {
		t.Fatalf("open: %s", r.Content)
	}
	s := snap.Run2(ctx, nil)
	if !strings.Contains(s.Content, "button") || !strings.Contains(s.Content, "[e") {
		t.Fatalf("snapshot missing button/ref: %s", s.Content)
	}
	ref := firstRef(s.Content)
	if ref == "" {
		t.Fatal("no ref parsed")
	}
	if r := click.Run2(ctx, map[string]any{"ref": ref}); r.IsError {
		t.Fatalf("click: %s", r.Content)
	}
	c := console.Run2(ctx, nil)
	if !strings.Contains(c.Content, "clicked!") && !strings.Contains(c.Content, "loaded") {
		t.Fatalf("console missing logs: %s", c.Content)
	}
}

func firstRef(snap string) string {
	i := strings.Index(snap, "[e")
	if i < 0 {
		return ""
	}
	j := strings.IndexByte(snap[i:], ']')
	if j < 0 {
		return ""
	}
	return snap[i+1 : i+j]
}
