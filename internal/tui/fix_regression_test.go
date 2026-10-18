package tui

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/elhenro/bee/internal/ask"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

func TestInit_ResumedTranscriptFlushedOnce(t *testing.T) {
	m := NewModel(nil, "/tmp/proj", "test-model", "workspace-write", "full")
	m = m.WithInitialMessages([]types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "hi"}}},
	})
	if m.Init() == nil {
		t.Fatal("expected init cmds")
	}
	nm, cmd := m.Update(initFlushMsg{})
	m = nm.(Model)
	if cmd == nil || m.printedCount != len(m.messages) {
		t.Fatalf("flush did not advance printedCount: %d/%d", m.printedCount, len(m.messages))
	}
	if again := m.flush(); again != nil {
		t.Fatal("second flush re-printed the resumed transcript")
	}
}

func TestModal_NonKeyMessagesFallThrough(t *testing.T) {
	m := newTestModel(t)
	m.approval.Show(ApprovalRequest{ToolName: "bash", Action: "x", UseID: "u1"})
	m.state = StateStreaming
	m.turnGen = 1
	nm, _ := m.Update(turnDoneMsg{gen: 1})
	if got := nm.(Model).state; got != StateIdle {
		t.Fatalf("turnDone swallowed by modal, state=%v", got)
	}
	nm, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if nm.(Model).width != 100 {
		t.Fatal("window size swallowed by modal")
	}
}

func TestApproval_QueuesConcurrentRequests(t *testing.T) {
	m := newTestModel(t)
	m.approval.Show(ApprovalRequest{UseID: "a"})
	m.approval.Show(ApprovalRequest{UseID: "b"})
	if m.approval.Request.UseID != "a" {
		t.Fatalf("second request overwrote first: %q", m.approval.Request.UseID)
	}
	var cmd tea.Cmd
	m.approval, cmd = m.approval.decide(ApprovalAllow)
	if dec := cmd().(ApprovalDecisionMsg); dec.UseID != "a" {
		t.Fatalf("decision for %q, want a", dec.UseID)
	}
	if !m.approval.Active || m.approval.Request.UseID != "b" {
		t.Fatal("queued request not shown after resolve")
	}
	m.approval, _ = m.approval.decide(ApprovalDeny)
	if m.approval.Active {
		t.Fatal("modal should close once the queue drains")
	}
}

func TestAsk_QueuesConcurrentQuestions(t *testing.T) {
	m := newTestModel(t)
	q := ask.Question{Options: []ask.Option{{Label: "x"}}}
	m.askModel.Show("a", q)
	m.askModel.Show("b", q)
	if m.askModel.useID != "a" {
		t.Fatalf("second question overwrote first: %q", m.askModel.useID)
	}
	var cmd tea.Cmd
	m.askModel, cmd = m.askModel.answer(ask.Answer{Index: 0})
	if got := cmd().(AskAnswerMsg); got.UseID != "a" {
		t.Fatalf("answer for %q, want a", got.UseID)
	}
	if !m.askModel.Active || m.askModel.useID != "b" {
		t.Fatal("queued question not shown after answer")
	}
}

func TestSide_EngineChangesRejectedWhileStreaming(t *testing.T) {
	eng := &loop.Engine{Cfg: config.Defaults()}
	m := &Model{eng: eng, state: StateStreaming}
	s := &tuiSide{m: m}
	if err := s.SwitchProviderModel("openrouter", "x"); err != errEngineBusy {
		t.Fatalf("SwitchProviderModel = %v, want busy", err)
	}
	if err := s.ForkSession(""); err == nil {
		t.Fatal("fork must be rejected mid-turn")
	}
	before := eng.Cfg.Role
	m.role = "scout"
	m.mirrorToEngine()
	if eng.Cfg.Role != before {
		t.Fatal("role written to the engine while a turn runs")
	}
	m.state = StateIdle
	m.mirrorToEngine()
	if eng.Cfg.Role != "scout" {
		t.Fatalf("role not applied once idle: %q", eng.Cfg.Role)
	}
}

func TestCancelTurn_ClearsStallResume(t *testing.T) {
	m := newTestModel(t)
	m.state = StateStreaming
	m.stallResumePending = true
	m.stallResumeText = "continue"
	m.cancelTurn()
	if m.stallResumePending || m.stallResumeText != "" || m.state != StateIdle {
		t.Fatal("stall resume survived cancel")
	}
}

func TestRenderRows_HighlightsMultibyteNames(t *testing.T) {
	names := []string{"größe-model"}
	matches := fuzzy.Find("ö", names)
	out := renderRows(matches, 0, "", "#", 60, func(i int) (string, string) { return names[i], "" })
	if !strings.Contains(out, "ö") || strings.ContainsRune(out, '�') {
		t.Fatalf("mojibake in %q", out)
	}
}

func TestHistory_MultilineRoundTrip(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	AppendHistory("one line")
	AppendHistory("first\nsecond \\n literal\r\nthird")
	got := LoadHistory()
	if len(got) != 2 || got[1] != "one line" || got[0] != "first\nsecond \\n literal\r\nthird" {
		t.Fatalf("round trip: %q", got)
	}
	// older files hold raw lines, backslashes included
	if dec := decodeHistory(`grep 'a\nb'`); dec != `grep 'a\nb'` {
		t.Fatalf("legacy line altered: %q", dec)
	}
}

func TestWidthHelpers_CountCells(t *testing.T) {
	if w := lipglossWidth("日本語"); w != 6 {
		t.Fatalf("width = %d, want 6", w)
	}
	if got := truncateVisible("日本語日本語", 5); lipgloss.Width(got) > 5 {
		t.Fatalf("truncate overflowed: %q", got)
	}
	for _, line := range wrapHanging("日本語日本語日本語 テスト", 8) {
		if lipgloss.Width(line) > 8 {
			t.Fatalf("wrapHanging line too wide: %q", line)
		}
	}
	if got := wrapHanging("héllo wörld ünïcode", 6); strings.ContainsRune(strings.Join(got, ""), '�') {
		t.Fatalf("split inside a rune: %q", got)
	}
}

func TestPadFrameLR_KeepsLeftPadOnClippedLines(t *testing.T) {
	m := newTestModel(t)
	m.outerPadLR = 2
	m.width = 20
	got := m.padFrameLR(strings.Repeat("x", 40))
	if !strings.HasPrefix(got, "  ") || lipgloss.Width(got) > 20 {
		t.Fatalf("clipped line lost its gutter: %q", got)
	}
}

func TestCSITranslator_KittyGeneralCSIu(t *testing.T) {
	cases := map[string]string{
		"\x1b[121;3u": "\x1by",      // alt+y
		"\x1b[13;3u":  "\x1b\r",     // alt+enter
		"\x1b[47;5u":  "\x1f",       // ctrl+/
		"\x1b[32;5u":  "\x00",       // ctrl+space
		"\x1b[97;4u":  "\x1bA",      // alt+shift+a
		"\x1b[99;6u":  "\x1b[99;6u", // ctrl+shift+c has no legacy form
		"\x1b[57358u": "\x1b[57358u",
		"\x1b[1;5A":   "\x1b[1;5A",
	}
	for in, want := range cases {
		got, held := translateModifyOtherKeys([]byte(in))
		if !bytes.Equal(got, []byte(want)) || held != nil {
			t.Errorf("%q: got %q held %q want %q", in, got, held, want)
		}
	}
}

func TestPalette_EnterWithoutMatchesFallsThrough(t *testing.T) {
	m := newTestModel(t)
	m.palette.Active = true
	m.palette.matches = nil
	nm, _, claimed := m.claimByPane(tea.KeyMsg{Type: tea.KeyEnter})
	if claimed || nm.palette.Active {
		t.Fatal("enter with no matches must close the palette and fall through")
	}
}

func TestAgentView_LettersAreReplyText(t *testing.T) {
	av := NewAgentView()
	av.Open()
	nv, cmd := av.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd != nil && cmd() == (CloseAgentViewMsg{}) {
		t.Fatal("q closed the agent view while the reply input has focus")
	}
	if nv.reply.Value() != "q" {
		t.Fatalf("reply = %q, want q", nv.reply.Value())
	}
}
