package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/types"
)

// resuming a session seeds the context-fill estimate from its history; the
// tracker reset that follows must not wipe it.
func TestOpenSession_SeedsContextFill(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	r, err := session.Open("sess-a")
	if err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("hello world ", 2000)
	if err := r.Append(context.Background(), types.Message{ID: "1", Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: big}}}); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()

	costs := cost.New()
	costs.Record("p", "m", 5000, 100) // the previous conversation's fill
	eng := &loop.Engine{Costs: costs}
	t.Cleanup(func() {
		if eng.Sessions != nil {
			_ = eng.Sessions.Close() // windows cannot delete the open rollout under TempDir
		}
	})
	m := NewModel(eng, "/tmp/work", "m", "", caveman.Default)
	if err := m.side().OpenSession("sess-a"); err != nil {
		t.Fatal(err)
	}
	got := costs.LastInput()
	if got == 0 || got == 5000 {
		t.Fatalf("LastInput = %d, want an estimate of the resumed history", got)
	}
	if n := costs.Total().Calls; n != 0 {
		t.Fatalf("old events survived the resume: %d calls", n)
	}
}
