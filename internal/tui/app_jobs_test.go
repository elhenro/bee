package tui

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/jobs"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// capProvider answers every request with a short final reply and keeps the
// transcript of each request it saw.
type capProvider struct {
	mu   sync.Mutex
	reqs [][]types.Message
}

func (p *capProvider) Name() string { return "stub" }
func (p *capProvider) Stream(_ context.Context, r llm.Request) (<-chan llm.Event, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, r.Messages)
	p.mu.Unlock()
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: llm.EventTextDelta, Delta: "ok"}
	ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
	close(ch)
	return ch, nil
}

func (p *capProvider) sawText(sub string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, req := range p.reqs {
		for _, m := range req {
			for _, b := range m.Content {
				if b.Type == types.BlockText && strings.Contains(b.Text, sub) {
					return true
				}
			}
		}
	}
	return false
}

// turnDoneOf finds the turnDoneMsg in a command batch. Unlike turnDoneIn it
// runs members concurrently: a batch from onJobsWake carries the blocking wake
// pump next to the turn, and running that one inline would hang.
func turnDoneOf(t *testing.T, cmd tea.Cmd) turnDoneMsg {
	t.Helper()
	found := make(chan turnDoneMsg, 1)
	var walk func(tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			switch msg := c().(type) {
			case turnDoneMsg:
				select {
				case found <- msg:
				default:
				}
			case tea.BatchMsg:
				for _, sub := range msg {
					walk(sub)
				}
			}
		}()
	}
	walk(cmd)
	select {
	case d := <-found:
		return d
	case <-time.After(10 * time.Second):
		t.Fatal("no turnDoneMsg")
		return turnDoneMsg{}
	}
}

func jobModel(t *testing.T) (Model, *jobs.Manager, *capProvider) {
	t.Helper()
	cfg := config.Defaults()
	cfg.Memory.Enabled = false
	jm := jobs.New()
	t.Cleanup(jm.Close)
	prov := &capProvider{}
	eng := &loop.Engine{Provider: prov, Tools: tools.NewRegistry(), Cfg: cfg, Cwd: t.TempDir(), Jobs: jm, Stdout: io.Discard}
	m := NewModel(eng, t.TempDir(), "m", "", caveman.Default)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return nm.(Model), jm, prov
}

func spawnJob(t *testing.T, jm *jobs.Manager, script string) {
	t.Helper()
	_, err := jm.Spawn(script, 0, func(ctx context.Context, out io.Writer) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "bash", "-c", script)
		cmd.Stdout, cmd.Stderr = out, out
		tools.GroupKill(cmd, time.Second)
		return cmd
	})
	if err != nil {
		t.Fatal(err)
	}
}

func awaitWake(t *testing.T, jm *jobs.Manager) {
	t.Helper()
	select {
	case <-jm.Wake():
	case <-time.After(5 * time.Second):
		t.Fatal("job never finished")
	}
}

// idle bee + finished job: a turn starts by itself and the model is handed the
// job's output, with no user input.
func TestJobsWake_IdleStartsTurnWithNotice(t *testing.T) {
	m, jm, prov := jobModel(t)
	spawnJob(t, jm, "echo built ok")
	awaitWake(t, jm)

	nm, cmd := m.Update(jobsWakeMsg{})
	m = nm.(Model)
	if m.state != StateStreaming {
		t.Fatalf("state = %v, want a turn started", m.state)
	}
	last := m.messages[len(m.messages)-1]
	if !strings.HasPrefix(last.Display, "(job 1 done, exit 0") {
		t.Fatalf("display = %q", last.Display)
	}
	if done := turnDoneOf(t, cmd); done.err != nil {
		t.Fatalf("turn: %v", done.err)
	}
	if !prov.sawText("built ok") {
		t.Fatal("model never saw the job output")
	}
	if jm.Pending() != 0 {
		t.Fatal("job still pending after its turn")
	}
}

// a wake while a turn runs must not start a second one: the engine takes the
// result itself.
func TestJobsWake_BusyLeavesJobForTheEngine(t *testing.T) {
	m, jm, _ := jobModel(t)
	spawnJob(t, jm, "true")
	awaitWake(t, jm)
	m.state = StateStreaming

	nm, _ := m.Update(jobsWakeMsg{})
	if jm.Pending() != 1 {
		t.Fatalf("pending = %d, busy wake consumed the job", jm.Pending())
	}
	if nm.(Model).state != StateStreaming {
		t.Fatal("state changed")
	}
}

// the job lands after the loop's last look but before the turn is reported
// done: onTurnDone starts the follow-up.
func TestTurnDone_StartsTurnForLateJob(t *testing.T) {
	m, jm, prov := jobModel(t)
	m.input.SetValue("first")
	nm, cmd := m.handleSubmit()
	m = nm.(Model)
	done := turnDoneOf(t, cmd)
	if done.err != nil {
		t.Fatalf("first turn: %v", done.err)
	}
	spawnJob(t, jm, "echo late")
	awaitWake(t, jm)

	nm, cmd = m.Update(done)
	m = nm.(Model)
	if m.state != StateStreaming {
		t.Fatalf("state = %v, want follow-up turn for the late job", m.state)
	}
	if done = turnDoneOf(t, cmd); done.err != nil {
		t.Fatalf("follow-up: %v", done.err)
	}
	if !prov.sawText("late") {
		t.Fatal("follow-up turn missed the job output")
	}
}

func TestJobsWake_NoEngineJobsIsNoop(t *testing.T) {
	m := NewModel(nil, t.TempDir(), "m", "", caveman.Default)
	if cmd := m.waitJobs(); cmd != nil {
		t.Fatal("no engine: no pump")
	}
	if nm, cmd := m.startJobTurn(); cmd != nil || nm.state != StateIdle {
		t.Fatal("no engine: no turn")
	}
}

func TestStatusJobsChip(t *testing.T) {
	m, jm, _ := jobModel(t)
	if got := drawStatusJobs(m); got != "" {
		t.Fatalf("idle chip = %q", got)
	}
	spawnJob(t, jm, "sleep 30")
	if got := drawStatusJobs(m); !strings.Contains(got, "1 job") {
		t.Fatalf("chip = %q", got)
	}
	spawnJob(t, jm, "sleep 30")
	if got := drawStatusJobs(m); !strings.Contains(got, "2 jobs") {
		t.Fatalf("chip = %q", got)
	}
}
