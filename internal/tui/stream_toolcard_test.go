package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/elhenro/bee/internal/types"
)

func useMsg(id, name string, in map[string]any) types.Message {
	return types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{
		{Type: types.BlockToolUse, Use: &types.ToolUse{ID: id, Name: name, Input: in}},
	}}
}

func resultMsg(id, content string, isErr bool) types.Message {
	return types.Message{Role: types.RoleTool, Content: []types.ContentBlock{
		{Type: types.BlockToolResult, Result: &types.ToolResult{UseID: id, Content: content, IsError: isErr}},
	}}
}

func numbered(n int, prefix string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%s %d\n", prefix, i)
	}
	return b.String()
}

func TestToolCard_ReadCollapsesToSummary(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	r.RenderMessage(useMsg("u1", "read", map[string]any{"path": "main.go"}))
	out := stripANSI(r.RenderMessage(resultMsg("u1", numbered(20, "code"), false)))
	for _, want := range []string{glyphTool + " Read  main.go", glyphResult + "  20 lines"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "code 1") {
		t.Fatalf("collapsed read must not echo file content: %q", out)
	}
	// verbose (ctrl+o re-print) shows the content
	m := resultMsg("u1", numbered(20, "code"), false)
	full := stripANSI(r.RenderMessageDetail(m, true))
	if !strings.Contains(full, "code 1") || !strings.Contains(full, "code 20") {
		t.Fatalf("detail render should show all rows: %q", full)
	}
}

func TestToolCard_BashPreviewsFiveRowsThenMore(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	r.RenderMessage(useMsg("u1", "bash", map[string]any{"command": "go test ./..."}))
	out := stripANSI(r.RenderMessage(resultMsg("u1", numbered(12, "ok"), false)))
	if got := strings.Count(out, "ok "); got != previewBash {
		t.Fatalf("want %d preview rows, got %d in %q", previewBash, got, out)
	}
	if !strings.Contains(out, fmt.Sprintf("%s +%d lines", glyphMore, 12-previewBash)) {
		t.Fatalf("missing overflow row in %q", out)
	}
	// result rows hang off the connector, continuation rows align under it
	lines := strings.Split(strings.TrimLeft(out, "\n"), "\n")
	if !strings.HasPrefix(lines[1], "   "+glyphResult+"  ok 1") || !strings.HasPrefix(lines[2], "      ok 2") {
		t.Fatalf("result rows not aligned under the connector: %q", lines)
	}
}

func TestOutcomeFor(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	cases := []struct {
		content string
		isErr   bool
		want    toolOutcome
	}{
		{"fine", false, outcomeDone},
		{"exit 1\nboom", true, outcomeFail},
		{"refused by user: nope", true, outcomeRefused},
		{"path x denied by write filter", true, outcomeRefused},
	}
	for _, c := range cases {
		res := types.ToolResult{Content: c.content, IsError: c.isErr}
		if got := outcomeFor(res, r.viewResult(types.ToolUse{}, res)); got != c.want {
			t.Errorf("outcomeFor(%q, err=%v) = %v, want %v", c.content, c.isErr, got, c.want)
		}
	}
}

func TestToolCard_FailedEditDropsDiff(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	r.RenderMessage(useMsg("u1", "edit", map[string]any{"path": "a.go", "old": "x", "new": "y"}))
	out := stripANSI(r.RenderMessage(resultMsg("u1", "old not found in a.go", true)))
	if strings.Contains(out, "+ y") || strings.Contains(out, "- x") {
		t.Fatalf("failed edit must not show the diff that didn't land: %q", out)
	}
	if !strings.Contains(out, "old not found") || !strings.Contains(out, "Update  a.go") {
		t.Fatalf("failed edit should show header + error: %q", out)
	}
}

func TestToolCard_ErrorRowsCapAtSix(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	r.RenderMessage(useMsg("u1", "bash", map[string]any{"command": "make"}))
	out := stripANSI(r.RenderMessage(resultMsg("u1", numbered(10, "err"), true)))
	if got := strings.Count(out, "err "); got != previewError {
		t.Fatalf("want %d error rows, got %d in %q", previewError, got, out)
	}
}

func TestToolCard_ExpandsTabsInResults(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	r.RenderMessage(useMsg("u1", "bash", map[string]any{"command": "ls"}))
	out := r.RenderMessage(resultMsg("u1", "a\tb", false))
	if strings.Contains(out, "\t") {
		t.Fatalf("tabs must not reach the terminal: %q", out)
	}
}

func TestToolCard_RowsClippedToWidth(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 40)
	r.RenderMessage(useMsg("u1", "bash", map[string]any{"command": "cat"}))
	out := stripANSI(r.RenderMessage(resultMsg("u1", strings.Repeat("z", 200), false)))
	for _, ln := range strings.Split(out, "\n") {
		if w := len([]rune(ln)); w > 40 {
			t.Fatalf("row %d cols exceeds terminal width: %q", w, ln)
		}
	}
	if !strings.Contains(out, glyphMore) {
		t.Fatalf("clipped row should end in an ellipsis: %q", out)
	}
}

func TestRenderMessage_ConsecutiveToolMessagesStackTight(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	text := types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{{Type: types.BlockText, Text: "go"}}}
	r.RenderMessage(text)
	r.RenderMessage(useMsg("u1", "bash", map[string]any{"command": "a"}))
	first := r.RenderMessage(resultMsg("u1", "x", false))
	r.RenderMessage(useMsg("u2", "bash", map[string]any{"command": "b"}))
	second := r.RenderMessage(resultMsg("u2", "y", false))
	if !strings.HasPrefix(first, "\n") {
		t.Fatalf("first tool card after prose opens with a blank row: %q", first)
	}
	if strings.HasPrefix(second, "\n") {
		t.Fatalf("second consecutive tool card must stack tight: %q", second)
	}
	// prose resets the grouping
	r.RenderMessage(text)
	r.RenderMessage(useMsg("u3", "bash", map[string]any{"command": "c"}))
	if third := r.RenderMessage(resultMsg("u3", "z", false)); !strings.HasPrefix(third, "\n") {
		t.Fatalf("tool card after prose opens with a blank row again: %q", third)
	}
}

func TestRenderMessageDetail_RestoresGroupingState(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	r.RenderMessage(useMsg("u1", "bash", map[string]any{"command": "a"}))
	r.RenderMessage(resultMsg("u1", "x", false))
	if !r.afterTool {
		t.Fatal("afterTool should be set after a tool card")
	}
	detail := r.RenderMessageDetail(resultMsg("u1", "x", false), true)
	if !strings.HasPrefix(detail, "\n") {
		t.Fatalf("a re-print always opens with a blank row: %q", detail)
	}
	if !r.afterTool {
		t.Fatal("detail re-print must not disturb grouping state")
	}
}

func TestRenderPendingTools_SkipsEscalateAndCaps(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 100)
	var uses []types.ToolUse
	uses = append(uses, types.ToolUse{ID: "e", Name: "escalate", Input: map[string]any{"reason": "x"}})
	for i := 0; i < 12; i++ {
		uses = append(uses, types.ToolUse{ID: fmt.Sprint(i), Name: "read", Input: map[string]any{"path": fmt.Sprintf("f%d.go", i)}})
	}
	out := stripANSI(r.RenderPendingTools(uses, 0))
	if strings.Contains(out, "escalate") {
		t.Fatalf("escalate is not a running tool: %q", out)
	}
	if rows := strings.Count(out, "\n") + 1; rows != maxPendingRows {
		t.Fatalf("want %d rows, got %d: %q", maxPendingRows, rows, out)
	}
	if !strings.Contains(out, "+5 more running") {
		t.Fatalf("missing overflow row: %q", out)
	}
}

func TestRenderPendingTools_BulletPulses(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256) // tests run colorless by default
	defer lipgloss.SetColorProfile(prev)
	r := NewStreamRenderer(DefaultStyles(), 100)
	uses := []types.ToolUse{{ID: "1", Name: "read", Input: map[string]any{"path": "a.go"}}}
	on, off := r.RenderPendingTools(uses, 0), r.RenderPendingTools(uses, 4)
	if on == off {
		t.Fatal("pending bullet should change style between pulse phases")
	}
	if stripANSI(on) != stripANSI(off) {
		t.Fatal("pulse must only restyle, not reflow")
	}
}

func TestToolTitle(t *testing.T) {
	if toolTitle("edit") != "Update" || toolTitle("web_fetch") != "Fetch" {
		t.Fatal("built-in titles")
	}
	if toolTitle("my_custom_tool") != "my_custom_tool" {
		t.Fatal("unknown tools keep their id so nothing is hidden")
	}
}

func TestSearchRow_SplitsPathFromMatch(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)
	r := NewStreamRenderer(DefaultStyles(), 100)
	hit, bare := r.searchRow("a/b.go:17:  ReadTimeout: 5"), r.searchRow("no-location line")
	if stripANSI(hit) != "a/b.go:17:  ReadTimeout: 5" || stripANSI(bare) != "no-location line" {
		t.Fatalf("text must survive the split: %q %q", stripANSI(hit), stripANSI(bare))
	}
	// path:line and the match are separate styled spans; a bare row is one
	if got := strings.Count(hit, "\x1b[0m"); got != 2 {
		t.Fatalf("hit row should carry two styles, got %d: %q", got, hit)
	}
	if got := strings.Count(bare, "\x1b[0m"); got != 1 {
		t.Fatalf("bare row should carry one style, got %d: %q", got, bare)
	}
}

func TestExpandTabs_AlignsToStops(t *testing.T) {
	got := expandTabs("f\t21\tgo.mod\nd\t96\tinternal\nf\t253\tmain.go")
	rows := strings.Split(got, "\n")
	want := []string{"f   21  go.mod", "d   96  internal", "f   253 main.go"}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("row %d = %q, want %q", i, rows[i], want[i])
		}
	}
	// escapes take no width: `ab` is two cells, so the tab pads two more
	if got := expandTabs("\x1b[31mab\x1b[0m\tc"); got != "\x1b[31mab\x1b[0m  c" {
		t.Fatalf("escape width counted: %q", got)
	}
	// wide runes count two cells
	if got := expandTabs("界\tc"); got != "界  c" {
		t.Fatalf("wide rune width wrong: %q", got)
	}
	if got := expandTabs("\t\tx"); got != "        x" {
		t.Fatalf("leading tabs = %q", got)
	}
	if got := expandTabs("no tabs here"); got != "no tabs here" {
		t.Fatalf("plain text changed: %q", got)
	}
}

func TestRenderMessage_UIEchoIsAQuietNotice(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	note := ephemeral("(image staged: 12 KB)")
	out := stripANSI(r.RenderMessage(note))
	if !strings.Contains(out, glyphSep+" image staged: 12 KB") {
		t.Fatalf("notice missing or still parenthesized: %q", out)
	}
	if strings.Contains(out, glyphBee) || strings.Contains(out, "(") {
		t.Fatalf("notice wears the assistant marker or parens: %q", out)
	}
	// real assistant text keeps its marker, even when it happens to be parenthetical
	real := types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{{Type: types.BlockText, Text: "(done)"}}}
	if out := stripANSI(r.RenderMessage(real)); !strings.Contains(out, glyphBee) {
		t.Fatalf("real assistant text lost its marker: %q", out)
	}
	// ephemeral command output is the app speaking to you: marker stays
	listing := ephemeral("/init, /compact\n/model")
	if out := stripANSI(r.RenderMessage(listing)); !strings.Contains(out, glyphBee) {
		t.Fatalf("command output lost its marker: %q", out)
	}
}

// the args budget floors at a few words, which overshoots a narrow terminal;
// the head row still has to fit or it wraps and tears the left edge
func TestCallRow_NeverWrapsOnANarrowTerminal(t *testing.T) {
	for _, w := range []int{30, 44, 60} {
		r := NewStreamRenderer(DefaultStyles(), w)
		use := types.ToolUse{ID: "b", Name: "bash", Input: map[string]any{
			"command": "gofmt -l . && go vet ./... && go build ./... && go test ./...",
		}}
		out := r.frame(strings.TrimRight(r.renderToolUse(use), "\n"), false, true)
		for _, ln := range strings.Split(out, "\n") {
			if got := lipgloss.Width(ln); got > w {
				t.Fatalf("width %d: call row is %d cells: %q", w, got, stripANSI(ln))
			}
		}
	}
}
