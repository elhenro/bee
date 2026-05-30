// Package goal powers the "/goal" completion-condition feature: an agent loop
// that keeps running turns until a fast model judges a user-specified condition
// met. This file holds pure state + bookkeeping; no llm import.
package goal

import (
	"fmt"
	"strings"
	"time"
)

const (
	DefaultMaxTurns  = 25
	DefaultMaxTokens = 2_000_000
)

// Verdict markers the headless goal loop and TUI print when a goal run ends.
// The bench harness classifies a run by matching these prefixes in captured
// output, so the exact strings are a contract: change them here and every
// emit/parse site follows from the same const.
const (
	AchievedPrefix = "✓ goal achieved: "
	StoppedPrefix  = "goal: stopped ("
)

// Caps bounds an auto-loop. Zero fields fall back to the package defaults.
type Caps struct {
	MaxTurns  int
	MaxTokens int
}

func (c Caps) turns() int {
	if c.MaxTurns > 0 {
		return c.MaxTurns
	}
	return DefaultMaxTurns
}

func (c Caps) tokens() int {
	if c.MaxTokens > 0 {
		return c.MaxTokens
	}
	return DefaultMaxTokens
}

// State holds an active goal. Zero value = inactive.
type State struct {
	Condition     string
	Active        bool
	StartedAt     time.Time
	Turns         int // auto-loop turns since set
	TokenBaseline int // cumulative session tokens at set time
	LastReason    string
	Caps          Caps
}

// Set activates a goal, resetting counters and timestamps. tokenBaseline is the
// cumulative session token count at set time so spend can be measured forward.
func (s *State) Set(cond string, tokenBaseline int, now time.Time) {
	s.Condition = strings.TrimSpace(cond)
	s.Active = true
	s.StartedAt = now
	s.Turns = 0
	s.TokenBaseline = tokenBaseline
	s.LastReason = ""
}

// Clear zeroes everything; Active becomes false.
func (s *State) Clear() {
	*s = State{}
}

// Tick records one auto-loop turn.
func (s *State) Tick() {
	s.Turns++
}

// TokensSpent reports tokens consumed since the goal was set, floored at 0.
func (s *State) TokensSpent(curCumTokens int) int {
	spent := curCumTokens - s.TokenBaseline
	if spent < 0 {
		return 0
	}
	return spent
}

// CapsExceeded reports whether turns or token-spend passed the caps. reason is a
// short human string, empty when within bounds.
func (s *State) CapsExceeded(curCumTokens int) (bool, string) {
	if s.Turns >= s.Caps.turns() {
		return true, fmt.Sprintf("turn cap reached (%d)", s.Caps.turns())
	}
	if s.TokensSpent(curCumTokens) >= s.Caps.tokens() {
		return true, fmt.Sprintf("token cap reached (%s)", humanTokens(s.Caps.tokens()))
	}
	return false, ""
}

// Status is the multi-line "/goal show" block.
func (s *State) Status(curCumTokens int, now time.Time) string {
	if !s.Active {
		return "goal: none set"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "goal: %s\n", s.Condition)
	fmt.Fprintf(&b, "turns: %d/%d\n", s.Turns, s.Caps.turns())
	fmt.Fprintf(&b, "tokens: %s/%s\n", humanTokens(s.TokensSpent(curCumTokens)), humanTokens(s.Caps.tokens()))
	fmt.Fprintf(&b, "elapsed: %s\n", now.Sub(s.StartedAt).Round(time.Second))
	reason := s.LastReason
	if reason == "" {
		reason = "(none yet)"
	}
	fmt.Fprintf(&b, "last check: %s", reason)
	return b.String()
}

// StatLine is a compact one-line status-bar indicator, e.g.
// "goal: fix the build · t3 · 12k".
func (s *State) StatLine(curCumTokens int) string {
	if !s.Active {
		return ""
	}
	return fmt.Sprintf("goal: %s · t%d · %s",
		truncate(s.Condition, 40), s.Turns, humanTokens(s.TokensSpent(curCumTokens)))
}

// IsClearWord reports whether a /goal subcommand word means clear.
func IsClearWord(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "clear", "stop", "off", "reset", "none", "cancel":
		return true
	}
	return false
}

// truncate shortens s to max runes, appending an ellipsis when cut.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

// humanTokens renders a token count compactly: 12k, 2.0M, 900.
func humanTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dk", n/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
