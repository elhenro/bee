package loop

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/jobs"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

func spawnSh(t *testing.T, m *jobs.Manager, script string) {
	t.Helper()
	_, err := m.Spawn(script, 0, func(ctx context.Context, out io.Writer) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "bash", "-c", script)
		cmd.Stdout, cmd.Stderr = out, out
		tools.GroupKill(cmd, time.Second)
		return cmd
	})
	if err != nil {
		t.Fatal(err)
	}
}

func joinText(m types.Message) string {
	var b strings.Builder
	for _, c := range m.Content {
		if c.Type == types.BlockText {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

func jobEngine(m *jobs.Manager, scope string) *Engine {
	cfg := config.Defaults()
	cfg.Sandbox = config.SandboxConfig{Scope: scope, Approval: "never"}
	return &Engine{
		Provider: &twoTurnProvider{},
		Tools:    tools.NewRegistry(),
		Memory:   stubMemStore{},
		Jobs:     m,
		Cfg:      cfg,
		Cwd:      ".",
	}
}

func TestJobNotice_FoldedIntoRunningTurn(t *testing.T) {
	m := jobs.New()
	defer m.Close()
	spawnSh(t, m, "echo built ok")
	<-m.Wake()

	eng := jobEngine(m, "danger-full-access")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := eng.Run(ctx, "go")
	if err != nil {
		t.Fatal(err)
	}
	var note *types.Message
	for i, msg := range res.Messages {
		if msg.Role == types.RoleUser && strings.HasPrefix(joinText(msg), "[job ") {
			note = &res.Messages[i]
		}
	}
	if note == nil {
		t.Fatalf("job notice missing from transcript: %+v", res.Messages)
	}
	if body := joinText(*note); !strings.Contains(body, "[job 1 done, exit 0,") || !strings.Contains(body, "built ok") {
		t.Fatalf("notice = %q", body)
	}
	if !strings.HasPrefix(note.Display, "(job 1 done, exit 0") {
		t.Fatalf("display = %q", note.Display)
	}
	if m.Pending() != 0 {
		t.Fatal("notice left the job pending")
	}
}

func TestJobNotice_NilManagerAndEmptyQueue(t *testing.T) {
	if _, _, ok := (&Engine{}).JobNotice(); ok {
		t.Fatal("nil manager must yield nothing")
	}
	m := jobs.New()
	defer m.Close()
	if _, _, ok := (&Engine{Jobs: m}).JobNotice(); ok {
		t.Fatal("empty queue must yield nothing")
	}
}

func TestJobNotice_HandedOutOnce(t *testing.T) {
	m := jobs.New()
	defer m.Close()
	spawnSh(t, m, "exit 3")
	<-m.Wake()
	eng := &Engine{Jobs: m, Cfg: config.Defaults()}
	text, display, ok := eng.JobNotice()
	if !ok || !strings.Contains(text, "[job 1 failed, exit 3,") || !strings.Contains(text, "(no output)") {
		t.Fatalf("text = %q ok=%v", text, ok)
	}
	if !strings.Contains(display, "failed, exit 3") {
		t.Fatalf("display = %q", display)
	}
	if _, _, again := eng.JobNotice(); again {
		t.Fatal("same job delivered twice")
	}
}

func TestJobBlock_RedactsWhenConfined(t *testing.T) {
	secret := "AKIAABCDEFGHIJKLMNOP"
	j := jobs.Info{ID: 2, Label: "env", State: jobs.Exited, Output: "key=" + secret}

	open := &Engine{Cfg: config.Defaults()}
	open.Cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access"}
	if !strings.Contains(open.jobBlock(j), secret) {
		t.Fatal("unconfined output should stay raw")
	}
	shut := &Engine{Cfg: config.Defaults()}
	shut.Cfg.Sandbox = config.SandboxConfig{Scope: "workspace-write"}
	if strings.Contains(shut.jobBlock(j), secret) {
		t.Fatal("confined output must be redacted")
	}
}

func TestJobDisplay_Several(t *testing.T) {
	got := jobDisplay([]jobs.Info{
		{ID: 1, State: jobs.Exited},
		{ID: 2, State: jobs.Exited, Exit: 1},
		{ID: 3, State: jobs.TimedOut},
	})
	want := "(3 background jobs finished: #1 done, #2 failed, #3 timed out)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestJobNotice_IsSynthetic(t *testing.T) {
	if !isSyntheticUserText("[job 4 done, exit 0, 2s] make") {
		t.Fatal("job notice must not count as a real user ask")
	}
}
