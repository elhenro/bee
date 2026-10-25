package hive

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/elhenro/bee/internal/atomicfile"
	"github.com/elhenro/bee/internal/beehome"
)

// Pheromone is the hive-wide state card every bee carries in its prompt for one
// run: the goal verbatim, the queen's plan and one status line per sibling. It
// is how a colony stays aligned without talking. Each worker re-reads it every
// turn (loop.Engine.PromptTail), so a sibling's finished report reaches it on
// its next round, and a sibling's row is done work it can build on instead of
// redoing. With a path set the card is mirrored to disk after every change so
// a human can watch the hive while it runs.
type Pheromone struct {
	mu    sync.Mutex
	path  string
	goal  string
	plan  []SubTask
	cells []cell
	seq   int
}

// cell is one sub-task's live row on the card.
type cell struct {
	state  State
	report Report
	done   bool
	seq    int // finish order, for the row cap
}

const (
	pheromoneGoalChars = 1000
	pheromoneTaskChars = 200
	// pheromoneMaxRows bounds the card on a wide fan-out (bee fan over 50
	// files): past it, a reader sees its own row, the most recent finishes,
	// and a count of the rest, so the card stays O(1) in prompt cost.
	pheromoneMaxRows = 12
)

// hiveContract is the exit contract every worker carries. Behavior comes from
// the structure, not the sentiment: a bee is told it is disposable and that
// only its report survives, so hiding a failure gains it nothing and stopping
// early with a precise hand-off is the winning move.
const hiveContract = `## hive contract
you are one bee of many, disposable. your report is your legacy: queen
synthesizes from reports, not from your work, and the next bee starts from
your report, not your transcript. end your final message with:
## report
status: done | partial | blocked
did: what changed, files touched, what is verified
left: what remains, or none
next: what the next bee must know: gotchas, failing checks, decisions
cannot finish? stop early with status: blocked and a precise next line
instead of retrying the same approach. a sibling's finished row on the
pheromone card is done work: build on it, do not redo it.`

// NewPheromone returns an empty card. path "" keeps it in memory only;
// otherwise every change is mirrored there (see PheromonePath).
func NewPheromone(path string) *Pheromone { return &Pheromone{path: path} }

// Reset starts a run: the goal verbatim and the plan, every row pending.
func (p *Pheromone) Reset(goal string, plan []SubTask) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.goal = strings.TrimSpace(goal)
	p.plan = append([]SubTask(nil), plan...)
	p.cells = make([]cell, len(plan))
	for i := range p.cells {
		p.cells[i].state = StatePending
	}
	p.mirror()
}

// Start marks sub-task idx running.
func (p *Pheromone) Start(idx int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if idx < 0 || idx >= len(p.cells) {
		return
	}
	p.cells[idx] = cell{state: StateRunning}
	p.mirror()
}

// Finish records sub-task idx's outcome; siblings see its report line.
func (p *Pheromone) Finish(idx int, res Result) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if idx < 0 || idx >= len(p.cells) {
		return
	}
	st := StateDone
	if res.Err != nil {
		st = StateFailed
	}
	p.seq++
	p.cells[idx] = cell{state: st, report: res.Report, done: true, seq: p.seq}
	p.mirror()
}

// Tail returns the per-turn prompt tail for the bee on sub-task idx: its role
// prompt, the hive contract and the live card. The closure re-renders on every
// call so the engine sees the current card each turn.
func (p *Pheromone) Tail(idx int, sub SubTask) func() string {
	head := ""
	if rp := strings.TrimSpace(sub.Role.SystemPrompt()); rp != "" {
		head = "## hive role\n" + rp + "\n\n"
	}
	head += hiveContract + "\n\n"
	return func() string { return head + p.Render(idx) }
}

// Render emits the card. self is the reading bee's sub-task index (its row is
// marked) or -1 for a reader with no row. Lowercase and terse: it rides every
// request.
func (p *Pheromone) Render(self int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.render(self)
}

func (p *Pheromone) render(self int) string {
	var b strings.Builder
	b.WriteString("[pheromone] hive state, shared by every bee this run. it changes as siblings finish; re-read it each turn.\n")
	if p.goal != "" {
		b.WriteString("goal: " + clip(p.goal, pheromoneGoalChars) + "\n")
	}
	if len(p.plan) > 0 {
		b.WriteString("plan:\n")
	}
	shown, hidden := p.visibleRows(self)
	for _, i := range shown {
		st := p.plan[i]
		fmt.Fprintf(&b, "%d. ", i+1)
		if st.Role != "" {
			b.WriteString(string(st.Role) + ": ")
		}
		b.WriteString(clip(oneLineOf(st.Task), pheromoneTaskChars))
		c := p.cells[i]
		row := string(c.state)
		if c.done {
			row = c.report.Line()
		}
		if i == self {
			row += ", you"
		}
		b.WriteString(" [" + row + "]\n")
	}
	if hidden != "" {
		b.WriteString(hidden + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// visibleRows picks which rows a reader sees. Up to pheromoneMaxRows every row
// prints in plan order. Past that: the reader's own row and the most recently
// finished ones (newest first, which is the freshest sibling news), plus one
// line counting the rest by state.
func (p *Pheromone) visibleRows(self int) (rows []int, rest string) {
	n := len(p.plan)
	if n <= pheromoneMaxRows {
		for i := range p.plan {
			rows = append(rows, i)
		}
		return rows, ""
	}
	keep := map[int]bool{}
	if self >= 0 && self < n {
		keep[self] = true
		rows = append(rows, self)
	}
	finished := make([]int, 0, n)
	for i, c := range p.cells {
		if c.done && !keep[i] {
			finished = append(finished, i)
		}
	}
	sort.Slice(finished, func(a, b int) bool { return p.cells[finished[a]].seq > p.cells[finished[b]].seq })
	for _, i := range finished {
		if len(rows) >= pheromoneMaxRows {
			break
		}
		keep[i] = true
		rows = append(rows, i)
	}
	counts := map[State]int{}
	for i, c := range p.cells {
		if !keep[i] {
			counts[c.state]++
		}
	}
	var parts []string
	for _, st := range [...]State{StatePending, StateRunning, StateDone, StateFailed, StateCanceled} {
		if counts[st] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[st], st))
		}
	}
	return rows, fmt.Sprintf("… %d more rows not shown: %s", n-len(rows), strings.Join(parts, ", "))
}

// mirror writes the card plus every finished report to path, temp+rename so a
// reader never sees a torn file. Best-effort: memory is the source of truth.
// Caller holds mu.
func (p *Pheromone) mirror() {
	if p.path == "" {
		return
	}
	var b strings.Builder
	b.WriteString(p.render(-1))
	b.WriteString("\n")
	for i, c := range p.cells {
		if !c.done {
			continue
		}
		fmt.Fprintf(&b, "\n### bee %d\n%s\n", i+1, c.report.Render())
	}
	dir := filepath.Dir(p.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	_ = atomicfile.Write(p.path, []byte(b.String()), 0o600)
}

// Tailed is a Runner that accepts a per-turn system-prompt tail. loop.Engine
// satisfies it; runner wrappers forward it. The queen hands every worker its
// pheromone card through this seam, so a Runner without it (test stubs) simply
// runs without the card.
type Tailed interface {
	SetPromptTail(func() string)
}

// PheromonePath returns where a hive run mirrors its card:
// <beeHome>/sessions/hive/<id>.pheromone.md. Nothing is created on disk.
func PheromonePath(id string) (string, error) {
	return beehome.Join("sessions", "hive", id+".pheromone.md")
}
