package browser

import (
	"context"
	"fmt"
	"sync"

	"github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const consoleRingMax = 500

// ConsoleMsg is one captured console entry.
type ConsoleMsg struct {
	Level string
	Text  string
}

// Session owns a single chromedp browser context, launched lazily on first
// use and reused across tool calls. Safe for sequential tool calls within a
// run (the engine drives tools one at a time).
type Session struct {
	chromePath string
	headless   bool
	confined   bool // when true, restrict navigation to public http/https hosts

	mu        sync.Mutex
	allocCtx  context.Context
	allocStop context.CancelFunc
	ctx       context.Context
	ctxStop   context.CancelFunc
	started   bool

	consoleMu sync.Mutex
	console   []ConsoleMsg
}

// NewSession returns an unstarted session. Chrome launches on first ensure().
func NewSession(chromePath string, headless bool) *Session {
	return &Session{chromePath: chromePath, headless: headless}
}

// ensure launches Chrome once and wires console capture. Subsequent calls are
// no-ops. Caller must hold s.mu.
func (s *Session) ensure() error {
	if s.started {
		return nil
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(s.chromePath),
		chromedp.Flag("headless", s.headless),
	)
	s.allocCtx, s.allocStop = chromedp.NewExecAllocator(context.Background(), opts...)
	s.ctx, s.ctxStop = chromedp.NewContext(s.allocCtx)
	// force the browser process to spin up now so launch errors surface here.
	if err := chromedp.Run(s.ctx); err != nil {
		s.allocStop()
		return fmt.Errorf("browser launch failed: %w", err)
	}
	s.listenConsole()
	s.started = true
	return nil
}

// listenConsole subscribes to console + log events on the target.
func (s *Session) listenConsole() {
	chromedp.ListenTarget(s.ctx, func(ev any) {
		switch e := ev.(type) {
		case *runtime.EventConsoleAPICalled:
			var b []byte
			for _, a := range e.Args {
				if len(a.Value) > 0 {
					b = append(b, a.Value...)
					b = append(b, ' ')
				}
			}
			s.pushConsole(string(e.Type), string(b))
		case *log.EventEntryAdded:
			s.pushConsole(string(e.Entry.Level), e.Entry.Text)
		}
	})
}

func (s *Session) pushConsole(level, text string) {
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	s.console = append(s.console, ConsoleMsg{Level: level, Text: text})
	if len(s.console) > consoleRingMax {
		s.console = s.console[len(s.console)-consoleRingMax:]
	}
}

func (s *Session) drainConsole() []ConsoleMsg {
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	out := s.console
	s.console = nil
	return out
}

// run executes chromedp actions against the (lazily launched) context.
func (s *Session) run(ctx context.Context, actions ...chromedp.Action) error {
	s.mu.Lock()
	if err := s.ensure(); err != nil {
		s.mu.Unlock()
		return err
	}
	runCtx := s.ctx
	s.mu.Unlock()
	return chromedp.Run(runCtx, actions...)
}

// Close tears down the browser. Safe to call when never started.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctxStop != nil {
		s.ctxStop()
	}
	if s.allocStop != nil {
		s.allocStop()
	}
}
