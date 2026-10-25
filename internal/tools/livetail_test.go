package tools

import (
	"sync"
	"testing"
	"time"
)

func TestLastRows(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"last n", "a\nb\nc\nd\ne\nf\n", "c\nd\ne\nf"},
		{"blank rows skipped", "a\n\n  \nb\n", "a\nb"},
		{"carriage return rewrites row", "10%\r50%\r100%\ndone\n", "100%\ndone"},
		{"crlf", "a\r\nb\r\n", "a\nb"},
		{"open row counts", "a\nb", "a\nb"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		if got := lastRows(c.in, 4); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

type tailLog struct {
	mu  sync.Mutex
	got []string
}

func (l *tailLog) add(s string) { l.mu.Lock(); l.got = append(l.got, s); l.mu.Unlock() }
func (l *tailLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.got...)
}

func TestTailWriter_ThrottlesAndSendsTrailing(t *testing.T) {
	var log tailLog
	w := NewTailWriter(log.add)
	defer w.Close()
	for i := 0; i < 50; i++ {
		_, _ = w.Write([]byte("line\n"))
	}
	_, _ = w.Write([]byte("last\n"))
	if n := len(log.all()); n != 1 {
		t.Fatalf("burst should send once immediately, sent %d", n)
	}
	time.Sleep(2 * liveTailEvery)
	got := log.all()
	if len(got) != 2 || got[1] != "line\nline\nline\nlast" {
		t.Fatalf("trailing report missing: %q", got)
	}
}

func TestTailWriter_CloseCancelsPending(t *testing.T) {
	var log tailLog
	w := NewTailWriter(log.add)
	_, _ = w.Write([]byte("a\n"))
	_, _ = w.Write([]byte("b\n"))
	w.Close()
	time.Sleep(2 * liveTailEvery)
	if n := len(log.all()); n != 1 {
		t.Fatalf("report after Close: %q", log.all())
	}
}

func TestTailWriter_SkipsUnchangedTail(t *testing.T) {
	var log tailLog
	w := NewTailWriter(log.add)
	defer w.Close()
	_, _ = w.Write([]byte("a\n"))
	time.Sleep(liveTailEvery + 20*time.Millisecond)
	_, _ = w.Write([]byte("\n\n"))
	if n := len(log.all()); n != 1 {
		t.Fatalf("blank output changed nothing, got %q", log.all())
	}
}
