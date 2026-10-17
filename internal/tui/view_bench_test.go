package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/types"
)

const benchPara = "The handler wires the mux, sets **explicit timeouts** and returns a configured `http.Server`. " +
	"Nothing else changes, so the diff stays small and easy to review.\n\n"

func benchModel(b *testing.B) Model {
	b.Helper()
	m := NewModel(nil, "/tmp/proj", "test-model", "workspace-write", caveman.Full)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	m.state = StateStreaming
	return m
}

// idle frame: what View costs on every keypress and tick.
func BenchmarkView_Idle(b *testing.B) {
	m := benchModel(b)
	m.state = StateIdle
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

// busy frame with a 4KB partial: the spinner ticks, the text is cached.
func BenchmarkView_StreamingTick(b *testing.B) {
	m := benchModel(b)
	m.partial = strings.Repeat(benchPara, 12)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.loaderFrame = i
		_ = m.View()
	}
}

// delta arrives: partial grows one chunk, paragraph boundaries re-render the
// settled prefix through markdown.
func BenchmarkView_StreamingDelta(b *testing.B) {
	m := benchModel(b)
	text := strings.Repeat(benchPara, 12)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.partial = text[:(i*7)%len(text)]
		_ = m.View()
	}
}

func BenchmarkRenderMessage_Markdown(b *testing.B) {
	r := NewStreamRenderer(DefaultStyles(), 120)
	msg := types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{
		{Type: types.BlockText, Text: strings.Repeat(benchPara, 8) + "```go\nfunc main() {}\n```\n"},
	}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = r.RenderMessage(msg)
	}
}

func BenchmarkRenderMessage_ToolCards(b *testing.B) {
	r := NewStreamRenderer(DefaultStyles(), 120)
	use := useMsg("u1", "edit", map[string]any{"path": "a.go", "old": "x := 1\n", "new": "x := 2\ny := 3\n"})
	res := resultMsg("u1", "replaced 1 occurrence", false)
	r.RenderMessage(use)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = r.RenderMessage(res)
	}
}
