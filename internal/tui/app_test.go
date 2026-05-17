package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/caveman"
)

// newTestModel builds a model with no engine and a sane terminal size.
func newTestModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(nil, "/tmp/proj", "test-model", "workspace-write", caveman.Full)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return updated.(Model)
}

// drainTurnDone unwraps the cmd returned by submit(): it's a tea.Batch of
// the engine cmd plus the loader-tick cmd. Returns the turnDoneMsg the
// engine half produced (nil if none found).
func drainTurnDone(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return msg
	}
	for _, c := range batch {
		if c == nil {
			continue
		}
		m := c()
		if _, isDone := m.(turnDoneMsg); isDone {
			return m
		}
	}
	return nil
}

func TestModel_InitialView_TopBar(t *testing.T) {
	m := newTestModel(t)
	out := stripANSI(m.View())
	for _, want := range []string{"🐝", "/tmp/proj"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in view: %q", want, out)
		}
	}
}

func TestModel_SubmitTransitionsToStreaming(t *testing.T) {
	m := newTestModel(t)
	// type into input
	for _, ch := range "hello" {
		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = m2.(Model)
	}
	if got := m.input.Value(); got != "hello" {
		t.Fatalf("input got %q", got)
	}
	// submit
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if m.state != StateStreaming {
		t.Fatalf("expected streaming, got %v", m.state)
	}
	if cmd == nil {
		t.Fatal("expected a submit cmd")
	}
	// drain the synthetic engine-less cmd; submit batches the turnDoneMsg
	// with the loader-tick cmd, so we have to fish the turnDone out.
	msg := drainTurnDone(t, cmd)
	if _, ok := msg.(turnDoneMsg); !ok {
		t.Fatalf("expected turnDoneMsg, got %T", msg)
	}
	m2, _ = m.Update(msg)
	m = m2.(Model)
	if m.state != StateIdle {
		t.Fatalf("expected idle after turnDone, got %v", m.state)
	}
	if len(m.messages) < 1 {
		t.Fatal("expected at least one message after turn")
	}
}

func TestModel_CtrlSlashCyclesCaveman(t *testing.T) {
	m := newTestModel(t)
	if m.caveLvl != caveman.Full {
		t.Fatalf("expected Full start, got %v", m.caveLvl)
	}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlUnderscore})
	m = m2.(Model)
	if m.caveLvl != caveman.Ultra {
		t.Fatalf("expected Ultra after one cycle, got %v", m.caveLvl)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlUnderscore})
	m = m2.(Model)
	if m.caveLvl != caveman.Off {
		t.Fatalf("expected Off after two cycles, got %v", m.caveLvl)
	}
}

func TestModel_CtrlPEmitsProviderSentinel(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if cmd == nil {
		t.Fatal("expected sentinel cmd")
	}
	if _, ok := cmd().(openProviderMsg); !ok {
		t.Fatalf("expected openProviderMsg, got %T", cmd())
	}
}

func TestModel_EscCancelsStreaming(t *testing.T) {
	m := newTestModel(t)
	m.state = StateStreaming
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)
	if m.state != StateIdle {
		t.Fatalf("esc should drop to idle, got %v", m.state)
	}
}

func TestModel_EmptySubmitNoop(t *testing.T) {
	m := newTestModel(t)
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if cmd != nil {
		t.Fatal("empty submit should not produce a cmd")
	}
	if m.state != StateIdle {
		t.Fatalf("state should stay idle, got %v", m.state)
	}
}

func TestModel_CtrlKOpensPalette(t *testing.T) {
	m := newTestModel(t)
	// dispatch Ctrl+K
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("expected sentinel cmd")
	}
	if _, ok := cmd().(openPaletteMsg); !ok {
		t.Fatalf("expected openPaletteMsg, got %T", cmd())
	}
	// route the message back into Model.Update — palette should activate
	m2, _ = m.Update(openPaletteMsg{})
	m = m2.(Model)
	if !m.palette.Active {
		t.Fatal("palette not active after openPaletteMsg")
	}
}

func TestModel_PaletteSelect_RunsCommand(t *testing.T) {
	m := newTestModel(t)
	// activate palette, then simulate selection of /help
	m2, _ := m.Update(openPaletteMsg{})
	m = m2.(Model)
	if !m.palette.Active {
		t.Fatal("palette did not activate")
	}
	m2, _ = m.Update(PaletteSelectMsg{Name: "help"})
	m = m2.(Model)
	if len(m.messages) == 0 {
		t.Fatal("help should have appended a message")
	}
	last := m.messages[len(m.messages)-1]
	if last.Role != "assistant" || !strings.Contains(last.Content[0].Text, "compact") {
		t.Errorf("unexpected help message: %+v", last)
	}
}

func TestModel_SubmitSlashHelpRunsCommand(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/help")
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	// /help is synchronous; the only Cmd returned is the tea.Println that
	// pushes the help text into terminal scrollback.
	if m.state != StateIdle {
		t.Fatalf("expected idle after slash run, got %v", m.state)
	}
	if len(m.messages) == 0 {
		t.Fatal("expected help output appended")
	}
	if !strings.Contains(m.messages[0].Content[0].Text, "/compact") {
		t.Errorf("missing /compact in help text: %q", m.messages[0].Content[0].Text)
	}
}

func TestModel_SubmitSlashUnknown_SetsError(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/nopenopenope")
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if m.state != StateError {
		t.Fatalf("expected error state, got %v", m.state)
	}
	if !strings.Contains(m.lastErr, "unknown command") {
		t.Errorf("expected unknown-command msg, got %q", m.lastErr)
	}
}

func TestModel_SubmitSlashNew_ClearsScrollback(t *testing.T) {
	m := newTestModel(t)
	// seed a message
	m.input.SetValue("hello")
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	// drain the echo (submit returns a batched cmd, fish out turnDone)
	if msg := drainTurnDone(t, cmd); msg != nil {
		m2, _ = m.Update(msg)
		m = m2.(Model)
	}
	if len(m.messages) == 0 {
		t.Fatal("setup: expected scrollback to have messages")
	}
	// now /new
	m.input.SetValue("/new")
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	// /new clears messages, then appends the "(/new done)" confirmation —
	// so we expect exactly one entry, the confirmation.
	if len(m.messages) != 1 {
		t.Fatalf("expected 1 confirmation message after /new, got %d", len(m.messages))
	}
	if !strings.Contains(m.messages[0].Content[0].Text, "/new done") {
		t.Errorf("expected confirmation, got %q", m.messages[0].Content[0].Text)
	}
}

func TestModel_SubmitSlashQuit_EmitsQuit(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/quit")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected tea.Quit cmd from /quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}
}

// TestModel_CtrlD_DoublePressQuits verifies the confirm flow: first ctrl+d
// arms (no cmd, no quit); second ctrl+d within the window emits tea.Quit.
func TestModel_CtrlD_DoublePressQuits(t *testing.T) {
	m := newTestModel(t)
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = m2.(Model)
	if cmd != nil {
		t.Fatal("first ctrl+d should not emit any cmd")
	}
	if !m.quitArmed {
		t.Fatal("first ctrl+d should arm the quit confirm")
	}
	m2, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("second ctrl+d should emit tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}
}

// TestModel_CtrlC_SinglePressQuits verifies ctrl+c stays single-press
// (POSIX cancel convention) and is not gated by the quit-confirm.
func TestModel_CtrlC_SinglePressQuits(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c should emit tea.Quit on first press")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}
}

// TestModel_CtrlD_OtherKeyDisarms verifies that any other key after a
// first ctrl+d clears the armed state — a stray ctrl+d shouldn't leave
// the program one keystroke from death.
func TestModel_CtrlD_OtherKeyDisarms(t *testing.T) {
	m := newTestModel(t)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = m2.(Model)
	if !m.quitArmed {
		t.Fatal("setup: first ctrl+d should arm")
	}
	// any other key — type a rune
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = m2.(Model)
	if m.quitArmed {
		t.Fatal("non-ctrl+d key should disarm the quit confirm")
	}
	// next ctrl+d should arm again, not quit
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = m2.(Model)
	if cmd != nil {
		t.Fatal("ctrl+d after disarm should arm again, not quit")
	}
	if !m.quitArmed {
		t.Fatal("ctrl+d after disarm should re-arm")
	}
}

// TestModel_CtrlD_FromPickerQuits verifies the global quit gate fires
// even when the model picker is active — the user must never be trapped.
func TestModel_CtrlD_FromPickerQuits(t *testing.T) {
	m := newTestModel(t)
	// activate picker manually since tests build with eng=nil
	m.picker = NewPicker(testCfg())
	m.picker.active = true
	if !m.picker.Active() {
		t.Fatal("setup: picker should be active")
	}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = m2.(Model)
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if cmd == nil {
		t.Fatal("expected tea.Quit from picker")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}
	_ = m2
}

// TestModel_EscDismissesPicker verifies the picker's Update is reached
// when active and that Esc collapses the picker (the user reported "esc
// does nothing"; the picker stage logic handles it correctly — the real
// trap was the CSI translator holding bare ESC, fixed in csi_input.go).
func TestModel_EscDismissesPicker(t *testing.T) {
	m := newTestModel(t)
	m.picker = NewPicker(testCfg())
	m.picker.active = true
	if !m.picker.Active() {
		t.Fatal("setup: picker should be active")
	}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)
	if m.picker.Active() {
		t.Fatal("picker should be inactive after esc on providers stage")
	}
}

// TestModel_CtrlD_FromApprovalQuits verifies the global gate works while
// the approval modal claims keys.
func TestModel_CtrlD_FromApprovalQuits(t *testing.T) {
	m := newTestModel(t)
	m.approval.Active = true
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = m2.(Model)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if cmd == nil {
		t.Fatal("expected tea.Quit from approval modal")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}
}

// TestModel_CtrlD_FromPaletteQuits verifies the global gate works while
// the slash palette is active.
func TestModel_CtrlD_FromPaletteQuits(t *testing.T) {
	m := newTestModel(t)
	m2, _ := m.Update(openPaletteMsg{})
	m = m2.(Model)
	if !m.palette.Active {
		t.Fatal("setup: palette should be active")
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = m2.(Model)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if cmd == nil {
		t.Fatal("expected tea.Quit from palette")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}
}

// hard-quit chords must always exit, even with buffer content / palette open.
func TestModel_CtrlC_AlwaysQuits(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/exit")          // non-empty buffer
	m.palette.Show("")                 // palette open
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected tea.Quit cmd from ctrl+c")
	}
}

func TestModel_CtrlD_AlwaysQuits(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("hello") // non-empty buffer — must not suppress quit
	// first press arms the confirm (no cmd yet).
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if cmd != nil {
		t.Fatal("first ctrl+d should arm, not quit")
	}
	// second press within window quits.
	_, cmd = m2.(Model).Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if cmd == nil {
		t.Fatal("expected tea.Quit cmd from second ctrl+d")
	}
}

func TestModel_SubmitSlashModel_ChangesLabel(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/model gpt-5")
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if m.model != "gpt-5" {
		t.Errorf("expected model=gpt-5, got %q", m.model)
	}
}

func TestModel_PendingImageAttachedAndCleared(t *testing.T) {
	m := newTestModel(t)
	// stage a fake image directly — we can't rely on the OS clipboard in CI.
	m.pendingImage = []byte{0x89, 0x50, 0x4e, 0x47}
	m.input.SetValue("describe this")
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("expected submit cmd")
	}
	if m.pendingImage != nil {
		t.Errorf("pendingImage should be cleared after submit; got %d bytes", len(m.pendingImage))
	}
	// last appended user message should contain a BlockImage block.
	if len(m.messages) == 0 {
		t.Fatal("no user message recorded")
	}
	last := m.messages[len(m.messages)-1]
	gotImage := false
	for _, b := range last.Content {
		if b.Type == "image" {
			gotImage = true
			if len(b.Data) == 0 {
				t.Error("image block has no bytes")
			}
		}
	}
	if !gotImage {
		t.Errorf("expected an image block on submit, got: %+v", last.Content)
	}
}

func TestBytesHuman(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{2048, "2 KiB"},
		{5 * 1024 * 1024, "5 MiB"},
	}
	for _, c := range cases {
		if got := bytesHuman(c.n); got != c.want {
			t.Errorf("bytesHuman(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestModel_StreamDeltaMsg_AccumulatesIntoPartial(t *testing.T) {
	m := newTestModel(t)
	// drive two deltas through Update and assert partial grows.
	m2, _ := m.Update(streamDeltaMsg{Delta: "hello "})
	m = m2.(Model)
	if m.partial != "hello " {
		t.Fatalf("partial after first delta = %q, want %q", m.partial, "hello ")
	}
	m2, _ = m.Update(streamDeltaMsg{Delta: "world"})
	m = m2.(Model)
	if m.partial != "hello world" {
		t.Fatalf("partial after second delta = %q, want %q", m.partial, "hello world")
	}
}

func TestModel_WithStreamCh_PumpsDelta(t *testing.T) {
	ch := make(chan string, 4)
	m := NewModel(nil, "/tmp/proj", "test-model", "workspace-write", caveman.Full).WithStreamCh(ch)
	// size + pump
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	cmd := m.waitStream()
	if cmd == nil {
		t.Fatal("waitStream returned nil with channel set")
	}
	ch <- "abc"
	msg := cmd()
	d, ok := msg.(streamDeltaMsg)
	if !ok {
		t.Fatalf("waitStream produced %T, want streamDeltaMsg", msg)
	}
	if d.Delta != "abc" {
		t.Errorf("delta = %q, want %q", d.Delta, "abc")
	}
}

func TestCycleMode(t *testing.T) {
	want := []string{"plan", "auto", "edit", "plan"}
	m := "edit"
	for i, w := range want {
		m = cycleMode(m, "openai")
		if m != w {
			t.Fatalf("step %d: want %q, got %q", i, w, m)
		}
	}
}

// local providers skip the auto stop — cycle is plan → edit → plan.
func TestCycleMode_LocalSkipsAuto(t *testing.T) {
	if got := cycleMode("plan", "ollama"); got != "edit" {
		t.Fatalf("plan→ollama want edit, got %q", got)
	}
	if got := cycleMode("edit", "ollama"); got != "plan" {
		t.Fatalf("edit→ollama want plan, got %q", got)
	}
	if got := cycleMode("plan", "openai"); got != "auto" {
		t.Fatalf("plan→openai want auto, got %q", got)
	}
}

// TestModel_MultilineInputRendersBothLines guards against a regression where a
// newline (ctrl+j / shift+enter) inside the input bar made only the second
// line visible — the textarea viewport hid row 0 because its height shrank to
// match the final logical-row count without rewinding YOffset.
func TestModel_MultilineInputRendersBothLines(t *testing.T) {
	m := newTestModel(t)
	// type "foo"
	for _, ch := range "foo" {
		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = m2.(Model)
	}
	// newline via ctrl+j (textarea InsertNewline binding)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = m2.(Model)
	// type "bar" on the new line
	for _, ch := range "bar" {
		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = m2.(Model)
	}
	if got := m.input.Value(); got != "foo\nbar" {
		t.Fatalf("input value = %q, want %q", got, "foo\nbar")
	}
	out := stripANSI(m.View())
	if !strings.Contains(out, "foo") {
		t.Errorf("rendered view missing first line %q; got:\n%s", "foo", out)
	}
	if !strings.Contains(out, "bar") {
		t.Errorf("rendered view missing second line %q; got:\n%s", "bar", out)
	}
}

// TestModel_LongSoftWrapKeepsFirstRow guards a related case: when a single
// line soft-wraps past the visible row count, the first visible row must not
// scroll off-screen behind the cursor.
func TestModel_LongSoftWrapKeepsFirstRow(t *testing.T) {
	m := newTestModel(t)
	// width-1 keystrokes to force a soft-wrap. Width is 80-4=76 after the
	// WindowSizeMsg minus the prompt (2 cells) → inner width is around 74.
	// 90 chars of "a" followed by a unique sentinel guarantees the sentinel
	// lives on the second visual row.
	chars := strings.Repeat("a", 80) + "TAILTOKEN"
	for _, ch := range chars {
		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = m2.(Model)
	}
	out := stripANSI(m.View())
	if !strings.Contains(out, "aaaaaaaa") {
		t.Errorf("rendered view missing first soft-wrap row; got:\n%s", out)
	}
	if !strings.Contains(out, "TAILTOKEN") {
		t.Errorf("rendered view missing second soft-wrap row; got:\n%s", out)
	}
}

// TestModel_MultilineInput_RenderedAcrossRepeatedViewCalls covers the value-
// receiver path: View() shrinks the local copy via syncInputHeight, but the
// persistent Model held by bubbletea must already carry the correct height
// from Update so a re-render without intervening Update still shows both
// lines. Regression: previously syncInputHeight ran only in View, so the
// persistent textarea kept height=1 (the NewModel default) until the next
// keystroke, which under specific YOffset states left row 0 clipped.
func TestModel_MultilineInput_RenderedAcrossRepeatedViewCalls(t *testing.T) {
	m := newTestModel(t)
	for _, ch := range "foo" {
		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = m2.(Model)
	}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = m2.(Model)
	for _, ch := range "bar" {
		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = m2.(Model)
	}
	// re-render twice without an intervening Update — pure View calls must
	// be idempotent and never clip a line.
	for i := 0; i < 2; i++ {
		out := stripANSI(m.View())
		if !strings.Contains(out, "foo") {
			t.Errorf("iter %d: missing first line; got:\n%s", i, out)
		}
		if !strings.Contains(out, "bar") {
			t.Errorf("iter %d: missing second line; got:\n%s", i, out)
		}
	}
}

// TestModel_MultilineInput_SurvivesViewBetweenKeys replicates the live
// bubbletea cycle: Update → View → Update → View. The textarea's viewport
// is a pointer field, so View's syncInputHeight mutation persists into the
// next Update via shared state — but the textarea.height value field stayed
// at the post-grow value, hiding the desync. Each Update must restore both
// fields so a newline inserted on the second keystroke renders both lines.
func TestModel_MultilineInput_SurvivesViewBetweenKeys(t *testing.T) {
	m := newTestModel(t)
	step := func(msg tea.Msg) {
		t.Helper()
		m2, _ := m.Update(msg)
		m = m2.(Model)
		// invoke View to mirror what bubbletea does between Update calls.
		_ = m.View()
	}
	for _, ch := range "foo" {
		step(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
	step(tea.KeyMsg{Type: tea.KeyCtrlJ})
	for _, ch := range "bar" {
		step(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
	step(tea.KeyMsg{Type: tea.KeyCtrlJ})
	for _, ch := range "baz" {
		step(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
	if got := m.input.Value(); got != "foo\nbar\nbaz" {
		t.Fatalf("input value = %q, want %q", got, "foo\nbar\nbaz")
	}
	out := stripANSI(m.View())
	for _, want := range []string{"foo", "bar", "baz"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered view missing %q; got:\n%s", want, out)
		}
	}
}

func TestCycleCaveman(t *testing.T) {
	want := []caveman.Level{caveman.Lite, caveman.Full, caveman.Ultra, caveman.Off, caveman.Lite}
	lvl := caveman.Off
	for i, w := range want {
		lvl = cycleCaveman(lvl)
		if lvl != w {
			t.Fatalf("step %d: want %v, got %v", i, w, lvl)
		}
	}
}
