package tui

import (
	"io"
	"os"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// Run launches the bubbletea program. Blocks until the model quits (q,
// ctrl+d after done). Stdout is the renderer target — engine output must be
// redirected elsewhere by the caller before this is called.
func (m *Model) Run() error {
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

// Quit signals shutdown so any goroutines that still call Model.send()
// no-op cleanly instead of panicking on a closed channel.
func (m *Model) Quit() {
	m.closeOne.Do(func() { close(m.closed) })
}

// EngineWriter returns a writer suitable for loop.Engine.Stdout. Engine
// deltas land in the log panel as dim body text. Lines are emitted as they
// arrive (newline-flushed) so partial streaming chunks don't explode the row
// count.
func (m *Model) EngineWriter() io.Writer {
	if m == nil {
		return os.Stdout
	}
	return &engineSink{m: m}
}

type engineSink struct {
	m   *Model
	mu  sync.Mutex
	buf []byte
}

// Write is io.Writer but the underlying engine may stream from worker
// goroutines (provider streaming, tool output), so a mutex around buf is
// required to keep the partial-line state consistent.
func (e *engineSink) Write(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.buf = append(e.buf, p...)
	for {
		i := indexByte(e.buf, '\n')
		if i < 0 {
			break
		}
		line := string(e.buf[:i])
		e.buf = e.buf[i+1:]
		if line == "" {
			continue
		}
		e.m.send(logMsg{level: "info", text: line})
	}
	return len(p), nil
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}
