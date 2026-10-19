package herdr

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

type call struct {
	method string
	params map[string]any
}

type fakeTransport struct {
	mu    sync.Mutex
	calls []call
	err   error
}

func (f *fakeTransport) send(method string, params map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, call{method, params})
	return nil
}

func (f *fakeTransport) snapshot() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

// states lists the reported states in order, "release" for a release call.
func (f *fakeTransport) states() []string {
	var out []string
	for _, c := range f.snapshot() {
		if c.method == "pane.release_agent" {
			out = append(out, "release")
			continue
		}
		out = append(out, c.params["state"].(string))
	}
	return out
}

func waitStates(t *testing.T, f *fakeTransport, want ...string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := f.states()
		if len(got) == len(want) {
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("states = %v, want %v", got, want)
				}
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("states = %v, want %v", f.states(), want)
}

func newTest(settle time.Duration) (*Reporter, *fakeTransport) {
	f := &fakeTransport{}
	return newReporter("w1:p1", f, settle), f
}

func TestTurnReportsWorkingThenIdle(t *testing.T) {
	r, f := newTest(10 * time.Millisecond)
	defer r.Close()
	end := r.Begin(Session{})
	waitStates(t, f, "working")
	end()
	waitStates(t, f, "working", "idle")
}

func TestIdleHeldBackAcrossBackToBackTurns(t *testing.T) {
	r, f := newTest(150 * time.Millisecond)
	defer r.Close()
	end := r.Begin(Session{})
	waitStates(t, f, "working")
	end()
	end = r.Begin(Session{}) // next turn lands inside the settle window
	time.Sleep(250 * time.Millisecond)
	end()
	waitStates(t, f, "working", "idle")
}

func TestReadyReportsIdleImmediately(t *testing.T) {
	r, f := newTest(time.Hour)
	defer r.Close()
	r.Ready(Session{ID: "s1"})
	waitStates(t, f, "idle")
}

func TestBlockOverridesWorkingAndCarriesMessage(t *testing.T) {
	r, f := newTest(10 * time.Millisecond)
	defer r.Close()
	end := r.Begin(Session{})
	waitStates(t, f, "working")
	unblock := r.Block("approve: rm -rf x")
	waitStates(t, f, "working", "blocked")
	if got := f.snapshot()[1].params["message"]; got != "approve: rm -rf x" {
		t.Fatalf("message = %v", got)
	}
	unblock()
	waitStates(t, f, "working", "blocked", "working")
	end()
	waitStates(t, f, "working", "blocked", "working", "idle")
}

func TestOverlappingTurnsStayWorkingUntilLastEnds(t *testing.T) {
	r, f := newTest(10 * time.Millisecond)
	defer r.Close()
	a := r.Begin(Session{})
	b := r.Begin(Session{})
	waitStates(t, f, "working")
	a()
	time.Sleep(60 * time.Millisecond)
	waitStates(t, f, "working")
	b()
	waitStates(t, f, "working", "idle")
}

func TestEndFuncIsIdempotent(t *testing.T) {
	r, f := newTest(10 * time.Millisecond)
	defer r.Close()
	a := r.Begin(Session{})
	b := r.Begin(Session{})
	a()
	a() // must not steal b's count
	time.Sleep(60 * time.Millisecond)
	waitStates(t, f, "working")
	b()
	waitStates(t, f, "working", "idle")
}

func TestSessionAndResumeArgvAttached(t *testing.T) {
	r, f := newTest(10 * time.Millisecond)
	defer r.Close()
	r.Begin(Session{ID: "abc", Path: "/s/abc.jsonl"})
	waitStates(t, f, "working")
	p := f.snapshot()[0].params
	if p["agent_session_id"] != "abc" || p["agent_session_path"] != "/s/abc.jsonl" {
		t.Fatalf("session params = %v", p)
	}
	argv, _ := p["resume_argv"].([]string)
	if len(argv) != 3 || argv[0] != "bee" || argv[1] != "back" || argv[2] != "abc" {
		t.Fatalf("resume_argv = %v", argv)
	}
	if p["pane_id"] != "w1:p1" || p["source"] != "herdr:bee" || p["agent"] != "bee" {
		t.Fatalf("identity params = %v", p)
	}
}

func TestSessionChangeReportedWhileIdle(t *testing.T) {
	r, f := newTest(10 * time.Millisecond)
	defer r.Close()
	r.Ready(Session{ID: "one"})
	waitStates(t, f, "idle")
	r.Ready(Session{ID: "two"})
	waitStates(t, f, "idle", "idle")
	if got := f.snapshot()[1].params["agent_session_id"]; got != "two" {
		t.Fatalf("session = %v", got)
	}
}

func TestSeqStrictlyIncreases(t *testing.T) {
	r, f := newTest(time.Millisecond)
	for i := 0; i < 5; i++ {
		end := r.Begin(Session{})
		end()
		time.Sleep(20 * time.Millisecond)
	}
	r.Close()
	var last int64
	for _, c := range f.snapshot() {
		seq := c.params["seq"].(int64)
		if seq <= last {
			t.Fatalf("seq %d not after %d", seq, last)
		}
		last = seq
	}
}

func TestCloseReleasesOnlyAfterAReport(t *testing.T) {
	r, f := newTest(10 * time.Millisecond)
	r.Close()
	if got := f.states(); len(got) != 0 {
		t.Fatalf("release without report: %v", got)
	}
	r2, f2 := newTest(10 * time.Millisecond)
	r2.Ready(Session{})
	waitStates(t, f2, "idle")
	r2.Close()
	r2.Close() // idempotent
	waitStates(t, f2, "idle", "release")
}

func TestNilReporterIsSafe(t *testing.T) {
	var r *Reporter
	r.Ready(Session{})
	r.Begin(Session{})()
	r.Block("x")()
	r.Close()
}

func TestFailedSendRetriedOnNextChange(t *testing.T) {
	r, f := newTest(10 * time.Millisecond)
	defer r.Close()
	f.mu.Lock()
	f.err = errTest
	f.mu.Unlock()
	r.Begin(Session{})
	time.Sleep(40 * time.Millisecond)
	f.mu.Lock()
	f.err = nil
	f.mu.Unlock()
	r.Begin(Session{}) // any change re-pushes the desired state
	waitStates(t, f, "working")
}

type testErr string

func (e testErr) Error() string { return string(e) }

const errTest = testErr("socket down")

func TestFromEnvGating(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("herdr reporting is disabled on windows")
	}
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	herdr := map[string]string{"HERDR_ENV": "1", "HERDR_SOCKET_PATH": "/x.sock", "HERDR_PANE_ID": "w1:p1"}
	with := func(extra ...string) map[string]string {
		m := map[string]string{}
		for k, v := range herdr {
			m[k] = v
		}
		for i := 0; i+1 < len(extra); i += 2 {
			m[extra[i]] = extra[i+1]
		}
		return m
	}
	cases := []struct {
		name string
		env  map[string]string
		tty  bool
		on   bool
	}{
		{"in pane on a tty", with(), true, true},
		{"not in herdr", map[string]string{}, true, false},
		{"missing pane id", with("HERDR_PANE_ID", ""), true, false},
		{"missing socket", with("HERDR_SOCKET_PATH", ""), true, false},
		{"nested non-interactive", with(), false, false},
		{"forced on without tty", with("BEE_HERDR", "1"), false, true},
		{"opt out", with("BEE_HERDR", "0"), true, false},
		{"opt out words", with("BEE_HERDR", "off"), true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := fromEnv(env(c.env), c.tty)
			if (r != nil) != c.on {
				t.Fatalf("enabled = %v, want %v", r != nil, c.on)
			}
			if r != nil {
				r.Close()
			}
		})
	}
}
