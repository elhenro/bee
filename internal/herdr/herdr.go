// Package herdr reports bee's lifecycle to the Herdr terminal multiplexer that
// hosts it, the way Herdr's built-in agent integrations do: a
// pane.report_agent call over the session's unix socket whenever the agent
// flips between working, blocked and idle. Herdr has no screen recognizer for
// bee, so without these reports a bee pane is just a shell.
//
// Everything is a no-op outside a Herdr pane, in a non-interactive process (a
// bee run from another agent's shell tool must not overwrite that agent's pane
// state) and with BEE_HERDR=0. BEE_HERDR=1 skips the interactivity check. All
// methods are safe on a nil *Reporter.
package herdr

import (
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

const (
	source = "herdr:bee"
	agent  = "bee"
	// settle holds an idle report back so back-to-back turns (goal loops,
	// auto-resume) don't flash a done badge between them.
	settle = 600 * time.Millisecond
)

type state string

const (
	idle    state = "idle"
	working state = "working"
	blocked state = "blocked"
)

// Session names the rollout Herdr should reopen with `bee back` after a
// restart. The zero value means "no session known yet".
type Session struct {
	ID   string
	Path string
}

type report struct {
	state   state
	message string
	session Session
}

type transport interface {
	send(method string, params map[string]any) error
}

type blocker struct {
	id  int
	msg string
}

// Reporter folds concurrent engine activity into one pane state: any open
// block wins, then any running turn, else idle. A single goroutine delivers
// the latest state, so a slow socket never stalls the agent loop.
type Reporter struct {
	pane   string
	tr     transport
	settle time.Duration

	mu     sync.Mutex
	active int
	nextID int
	blocks []blocker
	sess   Session

	// owned by the sender goroutine
	sent *report
	seq  int64

	wake chan struct{}
	quit chan struct{}
	done chan struct{}
	once sync.Once
}

var (
	defOnce sync.Once
	def     *Reporter
)

// Default is the process-wide reporter, nil when reporting is off.
func Default() *Reporter {
	defOnce.Do(func() { def = fromEnv(os.Getenv, interactive()) })
	return def
}

func interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) || term.IsTerminal(int(os.Stdout.Fd()))
}

func fromEnv(getenv func(string) string, tty bool) *Reporter {
	override := strings.ToLower(getenv("BEE_HERDR"))
	switch override {
	case "0", "off", "false", "no":
		return nil
	}
	// herdr talks named pipes on windows; not wired up.
	if getenv("HERDR_ENV") != "1" || runtime.GOOS == "windows" {
		return nil
	}
	sock, pane := getenv("HERDR_SOCKET_PATH"), getenv("HERDR_PANE_ID")
	if sock == "" || pane == "" {
		return nil
	}
	if !tty && override != "1" {
		return nil
	}
	return newReporter(pane, unixSender{path: sock}, settle)
}

func newReporter(pane string, tr transport, settle time.Duration) *Reporter {
	r := &Reporter{
		pane:   pane,
		tr:     tr,
		settle: settle,
		wake:   make(chan struct{}, 1),
		quit:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go r.loop()
	return r
}

// Ready announces the agent as idle, so the pane shows bee before its first
// turn, and records the session to resume.
func (r *Reporter) Ready(s Session) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if s.ID != "" {
		r.sess = s
	}
	r.mu.Unlock()
	r.kick()
}

// Begin marks a turn as running until the returned func is called. A non-zero
// s replaces the recorded session.
func (r *Reporter) Begin(s Session) func() {
	if r == nil {
		return func() {}
	}
	r.mu.Lock()
	r.active++
	if s.ID != "" {
		r.sess = s
	}
	r.mu.Unlock()
	r.kick()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			r.active--
			r.mu.Unlock()
			r.kick()
		})
	}
}

// Block marks the agent as waiting on the user (approval, question) until the
// returned func is called. msg is shown next to the blocked badge.
func (r *Reporter) Block(msg string) func() {
	if r == nil {
		return func() {}
	}
	r.mu.Lock()
	r.nextID++
	id := r.nextID
	r.blocks = append(r.blocks, blocker{id: id, msg: msg})
	r.mu.Unlock()
	r.kick()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			for i, b := range r.blocks {
				if b.id == id {
					r.blocks = append(r.blocks[:i], r.blocks[i+1:]...)
					break
				}
			}
			r.mu.Unlock()
			r.kick()
		})
	}
}

// Close hands the pane back to Herdr (the agent entry disappears) and waits
// briefly for the release to land. Idempotent.
func (r *Reporter) Close() {
	if r == nil {
		return
	}
	r.once.Do(func() { close(r.quit) })
	select {
	case <-r.done:
	case <-time.After(3 * time.Second):
	}
}

func (r *Reporter) kick() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *Reporter) desired() report {
	r.mu.Lock()
	defer r.mu.Unlock()
	rep := report{state: idle, session: r.sess}
	switch {
	case len(r.blocks) > 0:
		rep.state = blocked
		rep.message = r.blocks[0].msg
	case r.active > 0:
		rep.state = working
	}
	return rep
}

// Package-level shorthands over Default for call sites that hold no Reporter.

func Ready(s Session)         { Default().Ready(s) }
func Begin(s Session) func()  { return Default().Begin(s) }
func Block(msg string) func() { return Default().Block(msg) }
func Close()                  { Default().Close() }
