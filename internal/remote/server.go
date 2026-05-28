package remote

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// Engine is the minimal surface the relay needs to drive a session.
// The cmd layer adapts *loop.Engine to this.
type Engine interface {
	// Send runs ONE turn with the given user text. It streams assistant text
	// deltas via onDelta (may be called many times) and returns the final text.
	Send(ctx context.Context, text string, onDelta func(string)) (string, error)
}

// Msg is one transcript entry.
type Msg struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// Options configure a Server.
type Options struct {
	Addr  string    // listen addr, e.g. ":0" or "0.0.0.0:8765"; default ":0"
	Title string    // page title / shown in UI
	Log   io.Writer // activity log sink; nil => io.Discard
}

// sseEvent is one server-sent event queued for subscribers.
type sseEvent struct {
	typ  string
	data interface{}
}

// Server is a local web relay that drives a single bee session over LAN.
type Server struct {
	eng  Engine
	opts Options
	log  io.Writer

	mu          sync.Mutex
	history     []Msg
	subscribers map[int]chan sseEvent
	nextSub     int
	busy        bool
}

// New builds a Server. Defaults: Addr ":0", Title "bee remote", Log io.Discard.
func New(eng Engine, opts Options) *Server {
	if opts.Addr == "" {
		opts.Addr = ":0"
	}
	if opts.Title == "" {
		opts.Title = "bee remote"
	}
	logw := opts.Log
	if logw == nil {
		logw = io.Discard
	}
	return &Server{
		eng:         eng,
		opts:        opts,
		log:         logw,
		subscribers: make(map[int]chan sseEvent),
	}
}

// History returns a copy of the current transcript.
func (s *Server) History() []Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Msg, len(s.history))
	copy(out, s.history)
	return out
}

// Handler returns the http mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/events", s.handleEvents)
	mux.HandleFunc("/send", s.handleSend)
	return mux
}

func (s *Server) logf(format string, args ...interface{}) {
	fmt.Fprintf(s.log, "remote: "+format+"\n", args...)
}

// subscribe registers a buffered channel and returns it plus a history snapshot.
func (s *Server) subscribe() (chan sseEvent, int, []Msg) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextSub
	s.nextSub++
	ch := make(chan sseEvent, 64)
	s.subscribers[id] = ch
	replay := make([]Msg, len(s.history))
	copy(replay, s.history)
	return ch, id, replay
}

func (s *Server) unsubscribe(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.subscribers[id]; ok {
		delete(s.subscribers, id)
		close(ch)
	}
}

// broadcast sends to all subscribers without blocking; drops on full buffer.
func (s *Server) broadcast(ev sseEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subscribers {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Start binds a listener and returns it plus the best LAN URL to advertise.
func (s *Server) Start() (net.Listener, string, error) {
	ln, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		return nil, "", err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	host := LANIP()
	if host == "" {
		host = "localhost"
	}
	url := fmt.Sprintf("http://%s:%d", host, port)
	return ln, url, nil
}

// Serve blocks serving on ln until ctx is done, then shuts down gracefully.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{Handler: s.Handler()}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	case err := <-errc:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
