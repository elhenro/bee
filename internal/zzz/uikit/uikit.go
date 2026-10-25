// Package uikit holds what the zzz terminal UIs share: a lossy mailbox from
// Drive's goroutine into bubbletea and the log-line level heuristic.
package uikit

import (
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// Mailbox carries status messages from Drive's goroutine into Update. Sends
// never block or panic: a full buffer drops the message (events.jsonl is
// canonical) and a closed mailbox ignores it, so Drive may keep reporting
// while the program shuts down.
type Mailbox struct {
	C      chan tea.Msg
	closed chan struct{}
	once   sync.Once
}

// NewMailbox returns a mailbox buffering up to size messages.
func NewMailbox(size int) *Mailbox {
	return &Mailbox{C: make(chan tea.Msg, size), closed: make(chan struct{})}
}

// Send posts msg unless the mailbox is closed or full.
func (b *Mailbox) Send(msg tea.Msg) {
	select {
	case <-b.closed:
	default:
		select {
		case b.C <- msg:
		default:
		}
	}
}

// Wait is a command that surfaces the next posted message.
func (b *Mailbox) Wait() tea.Cmd { return func() tea.Msg { return <-b.C } }

// Close silences later sends.
func (b *Mailbox) Close() { b.once.Do(func() { close(b.closed) }) }

// LevelFor classifies a Println line for coloring: "err", "warn" or "info".
func LevelFor(s string) string {
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "fail"), strings.Contains(low, "error"):
		return "err"
	case strings.Contains(low, "blocked"), strings.Contains(low, "denied"):
		return "warn"
	}
	return "info"
}
