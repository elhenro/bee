package tui

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

// runs a Cmd the way the runtime would, flattening the batch and sequence
// messages it returns, and gives back what reaches the program in order
func playCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	v := reflect.ValueOf(msg)
	if v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeOf(cmd) {
		var out []tea.Msg
		for i := 0; i < v.Len(); i++ {
			out = append(out, playCmd(v.Index(i).Interface().(tea.Cmd))...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// the text a Cmd prints into scrollback
func printedText(cmd tea.Cmd) string {
	var parts []string
	for _, msg := range playCmd(cmd) {
		parts = append(parts, fmt.Sprintf("%+v", msg))
	}
	return strings.Join(parts, "\n")
}

func TestScrollback_BatchesKeepQueueOrder(t *testing.T) {
	s := &scrollback{}
	first, second := s.print("a"), s.print("b", "c")
	// whichever drain the runtime starts first takes everything queued so far
	got := printedText(second)
	if !strings.Contains(got, "a\nb\nc") {
		t.Fatalf("queue order lost: %q", got)
	}
	if again := printedText(first); strings.Contains(again, "a") {
		t.Fatalf("a drain that finds the queue empty must print nothing, got %q", again)
	}
}

func TestScrollback_NilFallsBackToPlainPrintln(t *testing.T) {
	var s *scrollback
	if got := printedText(s.print("a", "b")); !strings.Contains(got, "a\nb") {
		t.Fatalf("nil scrollback lost text: %q", got)
	}
	if s.print() != nil {
		t.Fatal("nothing to print should be a nil cmd")
	}
}

type orderProbe struct{ print func(i int) tea.Cmd }
type orderMsg int

func (orderProbe) Init() tea.Cmd { return nil }
func (p orderProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if n, ok := msg.(orderMsg); ok {
		return p, p.print(int(n))
	}
	return p, nil
}
func (orderProbe) View() string { return "" }

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// bubbletea runs each Cmd on its own goroutine: plain tea.Println cmds from
// back-to-back updates arrive swapped (about 5% of them in a burst this size).
// Through the queue every line shows up once, in order.
func TestScrollback_KeepsOrderAcrossUpdatesInARealProgram(t *testing.T) {
	const n = 2000
	var out syncBuf
	sb := &scrollback{}
	probe := orderProbe{print: func(i int) tea.Cmd { return sb.print(fmt.Sprintf("line-%05d", i)) }}
	p := tea.NewProgram(probe, tea.WithInput(nil), tea.WithOutput(&out), tea.WithoutSignalHandler())
	done := make(chan struct{})
	go func() { _, _ = p.Run(); close(done) }()
	time.Sleep(100 * time.Millisecond) // let the program start reading messages
	for i := 0; i < n; i++ {
		p.Send(orderMsg(i))
	}
	last := fmt.Sprintf("line-%05d", n-1)
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(out.String(), last) && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
	}
	p.Quit()
	<-done

	seen, swaps, prev := 0, 0, -1
	for _, m := range regexp.MustCompile(`line-(\d{5})`).FindAllStringSubmatch(out.String(), -1) {
		i, _ := strconv.Atoi(m[1])
		seen++
		if i <= prev {
			swaps++
		}
		prev = i
	}
	if seen != n || swaps != 0 {
		t.Fatalf("printed %d of %d lines with %d out of order", seen, n, swaps)
	}
}

// the interrupt note is queued after the turn's own messages, not before them
func TestInterruptNote_PrintsAfterTheTurnsMessages(t *testing.T) {
	m := newTestModel(t)
	m.state = StateStreaming
	m.interrupted = true
	m.messages = []types.Message{{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: "do the thing"}},
	}}
	_, cmd := m.Update(turnDoneMsg{err: context.Canceled, result: loop.RunResult{Messages: m.messages}})
	got := stripANSI(printedText(cmd))
	iMsg, iNote := strings.Index(got, "do the thing"), strings.Index(got, "interrupted")
	if iMsg < 0 || iNote < 0 || iMsg > iNote {
		t.Fatalf("want the turn first, then the note: %q", got)
	}
}
