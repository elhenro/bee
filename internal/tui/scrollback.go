package tui

import (
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// scrollback prints finalized text into the terminal's native scrollback in
// the order it was queued. bubbletea runs every returned Cmd on its own
// goroutine, so the tea.Println cmds of two back-to-back updates can reach
// the program swapped (a tool card landing under the next assistant line).
// Text is queued while Update runs, which is serial, and the drain cmd hands
// the queue to the program one batch at a time.
type scrollback struct {
	mu    sync.Mutex // guards queue
	send  sync.Mutex // held from take until done, so batches reach the program in turn
	queue []string
}

// queues texts and returns the Cmd that prints everything queued so far.
// Safe on a nil scrollback (a Model built without NewModel), which falls
// back to a plain tea.Println.
func (s *scrollback) print(texts ...string) tea.Cmd {
	if len(texts) == 0 {
		return nil
	}
	if s == nil {
		return tea.Println(strings.Join(texts, "\n"))
	}
	s.mu.Lock()
	s.queue = append(s.queue, texts...)
	s.mu.Unlock()
	return tea.Sequence(s.take, s.done)
}

// takes the queue as one print message. The runtime delivers the message to
// the program before it runs done, which is what keeps batches in order even
// when several drains are in flight. A drain that finds the queue empty (an
// earlier one took its text) prints nothing.
func (s *scrollback) take() tea.Msg {
	s.send.Lock()
	s.mu.Lock()
	text := strings.Join(s.queue, "\n")
	n := len(s.queue)
	s.queue = nil
	s.mu.Unlock()
	if n == 0 {
		return nil
	}
	return tea.Println(text)()
}

func (s *scrollback) done() tea.Msg {
	s.send.Unlock()
	return nil
}
