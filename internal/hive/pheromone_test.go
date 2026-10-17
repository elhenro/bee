package hive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPheromone_RenderRowsAndSelf(t *testing.T) {
	ph := NewPheromone("")
	plan := []SubTask{{Role: RoleBuilder, Task: "add foo\nsecond line"}, {Role: RoleSage, Task: "find bar"}, {Task: "no role"}}
	ph.Reset("  ship the feature  ", plan)
	ph.Start(0)
	ph.Finish(1, Result{Report: Report{Status: ReportDone, Did: "bar is in x.go"}})

	card := ph.Render(0)
	for _, want := range []string{
		"[pheromone]",
		"goal: ship the feature\n",
		"1. builder: add foo [running, you]",
		"2. sage: find bar [done: bar is in x.go]",
		"3. no role [pending]",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("card missing %q:\n%s", want, card)
		}
	}
	if strings.Contains(card, "second line") {
		t.Error("task text must be one line on the card")
	}
	if strings.Contains(ph.Render(-1), ", you") {
		t.Error("self marker printed for a reader with no row")
	}

	ph.Finish(0, Result{Err: errors.New("boom"), Report: Report{Status: ReportDied, Cause: "boom"}})
	if !strings.Contains(ph.Render(1), "1. builder: add foo [died: boom]") {
		t.Errorf("failed row:\n%s", ph.Render(1))
	}
	// out-of-range indices are ignored, not panics.
	ph.Start(9)
	ph.Finish(-1, Result{})
}

func TestPheromone_GoalClipped(t *testing.T) {
	ph := NewPheromone("")
	ph.Reset(strings.Repeat("g", pheromoneGoalChars+50), nil)
	if got := ph.Render(-1); !strings.Contains(got, "…") || len(got) > pheromoneGoalChars+200 {
		t.Errorf("goal not clipped: %d bytes", len(got))
	}
}

func TestPheromone_WideFanOutCapsRows(t *testing.T) {
	ph := NewPheromone("")
	plan := make([]SubTask, 40)
	for i := range plan {
		plan[i] = SubTask{Task: fmt.Sprintf("task-%02d", i)}
	}
	ph.Reset("", plan)
	for i := 0; i < 20; i++ {
		ph.Finish(i, Result{Report: Report{Status: ReportDone, Did: fmt.Sprintf("did-%02d", i)}})
	}
	ph.Start(30)

	card := ph.Render(30)
	rows := 0
	for _, l := range strings.Split(card, "\n") {
		if strings.Contains(l, " [") {
			rows++
		}
	}
	if rows > pheromoneMaxRows {
		t.Errorf("rows = %d, want <= %d:\n%s", rows, pheromoneMaxRows, card)
	}
	if !strings.Contains(card, "31. task-30 [running, you]") {
		t.Errorf("own row missing:\n%s", card)
	}
	// newest finishes shown, oldest hidden.
	if !strings.Contains(card, "did-19") || strings.Contains(card, "did-00") {
		t.Errorf("row choice wrong (want newest finishes):\n%s", card)
	}
	if !strings.Contains(card, "more rows not shown") || !strings.Contains(card, "19 pending") || !strings.Contains(card, "9 done") {
		t.Errorf("rest line wrong:\n%s", card)
	}
	// own row comes first.
	first := strings.SplitN(strings.SplitN(card, "plan:\n", 2)[1], "\n", 2)[0]
	if !strings.HasPrefix(first, "31. ") {
		t.Errorf("first row = %q, want the reader's own", first)
	}
}

func TestPheromone_TailIsLive(t *testing.T) {
	ph := NewPheromone("")
	ph.Reset("goal", []SubTask{{Role: RoleBuilder, Task: "a"}, {Role: RoleSage, Task: "b"}})
	tail := ph.Tail(0, SubTask{Role: RoleBuilder, Task: "a"})

	first := tail()
	for _, want := range []string{"## hive role", "you are builder", "## hive contract", "your report is your legacy", "## report", "goal: goal", "[pending, you]"} {
		if !strings.Contains(first, want) {
			t.Errorf("tail missing %q:\n%s", want, first)
		}
	}
	ph.Finish(1, Result{Report: Report{Status: ReportDone, Did: "b is done"}})
	if second := tail(); !strings.Contains(second, "[done: b is done]") {
		t.Errorf("tail did not pick up the sibling's finish:\n%s", second)
	}
	// no role: contract and card only.
	bare := ph.Tail(0, SubTask{Task: "a"})()
	if strings.Contains(bare, "## hive role") || !strings.Contains(bare, "## hive contract") {
		t.Errorf("bare tail:\n%s", bare)
	}
}

func TestPheromone_MirrorsToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "run.pheromone.md")
	ph := NewPheromone(path)
	ph.Reset("goal", []SubTask{{Role: RoleBuilder, Task: "a"}})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("mirror not written: %v", err)
	}
	if !strings.Contains(string(data), "1. builder: a [pending]") {
		t.Errorf("mirror content:\n%s", data)
	}
	ph.Finish(0, Result{Report: Report{Status: ReportDone, Did: "did a", Next: "nothing"}})
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "### bee 1\n## report\nstatus: done\ndid: did a\nnext: nothing") {
		t.Errorf("mirror lacks the full report:\n%s", data)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*.tmp"))
	if len(leftovers) != 0 {
		t.Errorf("temp files left behind: %v", leftovers)
	}
}

func TestPheromonePath_UnderBeeHome(t *testing.T) {
	t.Setenv(envBeeHome, "/tmp/beehome")
	p, err := PheromonePath("abc")
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join("/tmp/beehome", "sessions", "hive", "abc.pheromone.md") {
		t.Errorf("path = %q", p)
	}
}
