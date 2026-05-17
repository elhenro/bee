package zzz

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Status drives the single-line live status display + final summary. No
// TUI — just carriage-return overwrites to stderr so piping stdout still
// works (the engine's text deltas already go to stdout).
type Status struct {
	mu        sync.Mutex
	out       io.Writer
	startedAt time.Time
	iter      int
	maxIter   int
	phase     string
	tokens    TokenStat
	commits   int
	enabled   bool
}

// NewStatus returns a status renderer writing to w. Pass os.Stderr in
// normal CLI use, io.Discard in tests.
func NewStatus(w io.Writer) *Status {
	return &Status{out: w, startedAt: time.Now(), enabled: true}
}

// Disable suppresses live rendering (useful when --json or non-tty).
func (s *Status) Disable() { s.enabled = false }

func (s *Status) SetIter(n, max int) {
	s.mu.Lock()
	s.iter = n
	s.maxIter = max
	s.mu.Unlock()
	s.render()
}

func (s *Status) SetPhase(p string) {
	s.mu.Lock()
	s.phase = p
	s.mu.Unlock()
	s.render()
}

func (s *Status) SetTokens(t TokenStat) {
	s.mu.Lock()
	s.tokens = t
	s.mu.Unlock()
	s.render()
}

func (s *Status) IncCommits() {
	s.mu.Lock()
	s.commits++
	s.mu.Unlock()
	s.render()
}

// Println prints msg on its own line, leaving the status line intact below.
func (s *Status) Println(msg string) {
	if !s.enabled {
		fmt.Fprintln(s.out, msg)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.out, "\r\033[K%s\n", msg)
	s.renderLocked()
}

func (s *Status) render() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.renderLocked()
}

func (s *Status) renderLocked() {
	if !s.enabled {
		return
	}
	elapsed := time.Since(s.startedAt).Truncate(time.Second)
	fmt.Fprintf(s.out, "\r\033[K[zzz] iter=%d/%d phase=%s commits=%d tok=%d/%d  $%.4f  t+%s",
		s.iter, s.maxIter, s.phase, s.commits, s.tokens.Input, s.tokens.Output, s.tokens.USD, elapsed)
}

// RenderSummary prints the multi-line exit summary. Called once at end.
func (s *Status) RenderSummary(r *Run) {
	if s.enabled {
		fmt.Fprintln(s.out)
	}
	dur := r.EndedAt.Sub(r.StartedAt).Truncate(time.Second)
	w := s.out
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "═══════════════ zzz summary ═══════════════")
	fmt.Fprintf(w, "  id        : %s\n", r.ID)
	fmt.Fprintf(w, "  branch    : %s\n", r.Branch)
	fmt.Fprintf(w, "  mode      : %s\n", r.Mode)
	fmt.Fprintf(w, "  status    : %s\n", r.Status)
	fmt.Fprintf(w, "  cause     : %s\n", r.StopCause)
	fmt.Fprintf(w, "  iters     : %d\n", r.IterCount)
	fmt.Fprintf(w, "  commits   : %d\n", len(r.Commits))
	fmt.Fprintf(w, "  tokens    : %d in / %d out  ($%.4f)\n", r.Tokens.Input, r.Tokens.Output, r.Tokens.USD)
	fmt.Fprintf(w, "  duration  : %s\n", dur)
	fmt.Fprintf(w, "  inspect   : ~/.bee/zzz/runs/%s/\n", r.ID)
	fmt.Fprintln(w, "═══════════════════════════════════════════")
}
