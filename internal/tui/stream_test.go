package tui

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/elhenro/bee/internal/types"
)

// stripANSI removes CSI + OSC sequences so assertions match plain content.
// Uses charmbracelet/x/ansi so OSC 133 prompt-zone marks emitted by
// RenderMessage are stripped along with the usual color CSIs.
func stripANSI(s string) string { return ansi.Strip(s) }

func TestParseLoaderStyle_NamedValues(t *testing.T) {
	cases := map[string]LoaderStyle{
		"":         LoaderStyleDefault,
		"default":  LoaderStyleDefault,
		"swarm":    LoaderStyleSwarm,
		"bee":      LoaderStyleSwarm,
		"pulse":    LoaderStylePulse,
		"wave":     LoaderStyleWave,
		"comb":     LoaderStyleWave, // legacy alias
		"comet":    LoaderStyleComet,
		"hex":      LoaderStyleHex,
		"ripple":   LoaderStyleRipple,
		"rain":     LoaderStyleRain,
		"drip":     LoaderStyleRain, // legacy alias
		"honey":    LoaderStyleRain,
		"orbit":    LoaderStyleOrbit,
		"dance":    LoaderStyleOrbit, // legacy alias
		"waggle":   LoaderStyleOrbit,
		"breath":   LoaderStyleBreath,
		"stars":    LoaderStyleStars,
		"cosmic":   LoaderStyleStars,
		"forage":   LoaderStyleForage,
		"hive":     LoaderStyleForage,
		"figure8":  LoaderStyleFigure8,
		"fig8":     LoaderStyleFigure8,
		"vortex":   LoaderStyleVortex,
		"tornado":  LoaderStyleVortex,
		"gust":     LoaderStyleGust,
		"wind":     LoaderStyleGust,
		"scatter":  LoaderStyleScatter,
		"alarm":    LoaderStyleScatter,
		"flock":    LoaderStyleFlock,
		"boids":    LoaderStyleFlock,
		"dna":      LoaderStyleDNA,
		"helix":    LoaderStyleDNA,
		"matrix":   LoaderStyleMatrix,
		"cascade":  LoaderStyleMatrix,
		"heartbeat": LoaderStyleHeartbeat,
		"ekg":      LoaderStyleHeartbeat,
		"lightning": LoaderStyleLightning,
		"bolt":     LoaderStyleLightning,
		"snake":    LoaderStyleSnake,
		"chase":    LoaderStyleSnake,
		"fireworks": LoaderStyleFireworks,
		"burst":    LoaderStyleFireworks,
		"drunk":    LoaderStyleDrunk,
		"tipsy":    LoaderStyleDrunk,
		"jar":      LoaderStyleJar,
		"trapped":  LoaderStyleJar,
		"conga":    LoaderStyleConga,
		"party":    LoaderStyleConga,
		"queen":    LoaderStyleQueen,
		"royal":    LoaderStyleQueen,
		"drip2":    LoaderStyleDrip,
		"droplet":  LoaderStyleDrip,
		"marathon": LoaderStyleMarathon,
		"race":     LoaderStyleMarathon,
	}
	for in, want := range cases {
		if got := ParseLoaderStyle(in); got != want {
			t.Errorf("ParseLoaderStyle(%q) = %v want %v", in, got, want)
		}
	}
}

func TestParseLoaderStyle_UnknownFallsBackToDefault(t *testing.T) {
	for _, in := range []string{"????", "nonsense"} {
		if got := ParseLoaderStyle(in); got != LoaderStyleDefault {
			t.Errorf("ParseLoaderStyle(%q) = %v, want default", in, got)
		}
	}
}

func TestParseLoaderStyle_RandomInRange(t *testing.T) {
	// random picks one of the named painters — must be a known style, not
	// out-of-bounds.
	for i := 0; i < 40; i++ {
		s := ParseLoaderStyle("random")
		if s < LoaderStylePulse || s > LoaderStyleMarathon {
			t.Errorf("ParseLoaderStyle(random) out of named range: %v", s)
		}
	}
}

func TestRenderLoader_VariantsHaveDistinctOutput(t *testing.T) {
	styles := DefaultStyles()
	r := NewStreamRenderer(styles, 80)

	r.SetLoaderStyle(LoaderStyleSwarm)
	swarm := stripANSI(r.renderLoader(5))
	r.SetLoaderStyle(LoaderStyleWave)
	wave := stripANSI(r.renderLoader(5))
	r.SetLoaderStyle(LoaderStyleComet)
	comet := stripANSI(r.renderLoader(5))
	r.SetLoaderStyle(LoaderStyleRain)
	rain := stripANSI(r.renderLoader(5))

	out := []string{swarm, wave, comet, rain}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[i] == out[j] {
				t.Errorf("variants %d and %d produced identical output: %q", i, j, out[i])
			}
		}
	}
}

func TestRenderCompacting_NonEmpty(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	// frame 0: bar starts unfolded — output should be non-empty and
	// contain at least the lead bee glyph.
	got0 := stripANSI(r.RenderCompacting(0))
	if !strings.Contains(got0, "⬢") {
		t.Fatalf("frame 0 should contain lead glyph, got %q", got0)
	}
	// mid-cycle: braille canvas should have visible pixels (non-blank).
	gotMid := stripANSI(r.RenderCompacting(20))
	if got0 == gotMid {
		t.Errorf("frames 0 and 20 produced identical output — animation stuck?")
	}
}

func TestSanitizeControl_StripsBinaryBytes(t *testing.T) {
	in := "ok\x00\x07\x08\rnot\tok\x1b"
	got := sanitizeControl(in)
	if strings.ContainsAny(got, "\x00\x07\x08\r\x1b") {
		t.Fatalf("control bytes leaked: %q", got)
	}
	if got != "oknot\tok" {
		t.Fatalf("want %q, got %q", "oknot\tok", got)
	}
}

func TestCompactLines_DropsBlanksAfterSanitize(t *testing.T) {
	// raw line composed of nothing but NULs should collapse away
	lines := compactLines("real\n\x00\x00\x00\nrest")
	if len(lines) != 2 || lines[0] != "real" || lines[1] != "rest" {
		t.Fatalf("want [real rest], got %v", lines)
	}
}

func TestRenderMessage_UserText(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	m := types.Message{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: "hello bee"}},
	}
	out := stripANSI(r.RenderMessage(m))
	if !strings.Contains(out, "▸") {
		t.Fatalf("missing you marker in %q", out)
	}
	if !strings.Contains(out, "hello bee") {
		t.Fatalf("missing text in %q", out)
	}
}

func TestRenderMessage_AssistantWithToolUse(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	m := types.Message{
		Role: types.RoleAssistant,
		Content: []types.ContentBlock{
			{Type: types.BlockText, Text: "running bash"},
			{Type: types.BlockToolUse, Use: &types.ToolUse{
				ID:    "u1",
				Name:  "bash",
				Input: map[string]any{"cmd": "go test ./..."},
			}},
		},
	}
	out := stripANSI(r.RenderMessage(m))
	for _, want := range []string{"running bash", "◇", "bash", "cmd"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
	// assistant prose no longer carries a ⬢ prefix — only the tool block
	// keeps its ◇ marker.
	if strings.Contains(out, "⬢") {
		t.Fatalf("unexpected ⬢ on assistant turn in %q", out)
	}
}

func TestRenderMessage_ToolResultTruncates(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	// build a result with previewLinesCompact*2 lines to force the clamp
	totalLines := previewLinesCompact * 2
	var b strings.Builder
	for i := 0; i < totalLines; i++ {
		b.WriteString("line\n")
	}
	m := types.Message{
		Role: types.RoleTool,
		Content: []types.ContentBlock{{Type: types.BlockToolResult, Result: &types.ToolResult{
			UseID:   "u1",
			Content: b.String(),
		}}},
	}
	out := stripANSI(r.RenderMessage(m))
	// overflow signalled inline as `+N more`, no dedicated truncation row
	wantMore := fmt.Sprintf("+%d more", totalLines-previewLinesCompact)
	if !strings.Contains(out, wantMore) {
		t.Fatalf("expected %q overflow marker: %q", wantMore, out)
	}
	// at most previewLinesCompact "line" tokens shown (default compact mode)
	if got := strings.Count(out, "line"); got > previewLinesCompact {
		t.Fatalf("expected ≤%d line previews, got %d in %q", previewLinesCompact, got, out)
	}
}

func TestRenderMessage_TextAndToolUseSeparated(t *testing.T) {
	// regression: text block + tool_use must NOT share a row. Prior bug
	// rendered `<text>◇ write` because renderText omitted a trailing \n.
	r := NewStreamRenderer(DefaultStyles(), 80)
	m := types.Message{
		Role: types.RoleAssistant,
		Content: []types.ContentBlock{
			{Type: types.BlockText, Text: "Now creating the tool."},
			{Type: types.BlockToolUse, Use: &types.ToolUse{
				ID:    "u1",
				Name:  "write",
				Input: map[string]any{"path": "/tmp/x"},
			}},
		},
	}
	out := stripANSI(r.RenderMessage(m))
	// RenderMessage prepends a Spacer(1) "\n"; skip it before row indexing.
	body := strings.TrimLeft(out, "\n")
	lines := strings.Split(body, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected text and tool_use on separate rows, got: %q", body)
	}
	// row 0 holds the assistant glyph + text; row 1 holds the tool card.
	if !strings.Contains(lines[0], "Now creating the tool.") {
		t.Fatalf("row 0 should hold text body: %q", lines[0])
	}
	if !strings.Contains(lines[1], "write") {
		t.Fatalf("row 1 should hold tool card: %q", lines[1])
	}
}

func TestCollapseBlankRuns_CapsAtOne(t *testing.T) {
	in := "a\n\n\n\n\nb\n\n\nc"
	got := collapseBlankRuns(in)
	want := "a\n\nb\n\nc"
	if got != want {
		t.Fatalf("collapseBlankRuns: got %q want %q", got, want)
	}
}

func TestRenderMessage_CompactsModelBlankSpam(t *testing.T) {
	// regression: model emitting `text\n\n\n\n\n\n\nmore` used to print
	// every blank row into terminal scrollback via tea.Println. Cap at one.
	r := NewStreamRenderer(DefaultStyles(), 80)
	m := types.Message{
		Role: types.RoleAssistant,
		Content: []types.ContentBlock{
			{Type: types.BlockText, Text: "first paragraph\n\n\n\n\n\n\nsecond paragraph"},
		},
	}
	out := stripANSI(r.RenderMessage(m))
	// at most one consecutive blank line should remain
	if strings.Contains(out, "\n\n\n") {
		t.Fatalf("expected blank runs collapsed, got: %q", out)
	}
}

func TestRenderToolResult_VerboseShowsAllLines(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	r.SetVerbose(true)
	var b strings.Builder
	for i := 0; i < 5; i++ {
		b.WriteString("line\n")
	}
	m := types.Message{
		Role: types.RoleTool,
		Content: []types.ContentBlock{{Type: types.BlockToolResult, Result: &types.ToolResult{
			UseID:   "u1",
			Content: b.String(),
		}}},
	}
	out := stripANSI(r.RenderMessage(m))
	if strings.Contains(out, "more") {
		t.Fatalf("verbose: did not expect overflow marker: %q", out)
	}
	if got := strings.Count(out, "line"); got != 5 {
		t.Fatalf("verbose: expected 5 line previews, got %d in %q", got, out)
	}
}

func TestRenderToolResult_StripsBlankLines(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	m := types.Message{
		Role: types.RoleTool,
		Content: []types.ContentBlock{{Type: types.BlockToolResult, Result: &types.ToolResult{
			UseID:   "u1",
			Content: "first\n\n\n  \n",
		}}},
	}
	out := stripANSI(r.RenderMessage(m))
	// single non-blank line fits compact preview — no overflow marker.
	if strings.Contains(out, "more") {
		t.Fatalf("did not expect overflow marker: %q", out)
	}
	// RenderMessage prepends a Spacer(1) "\n"; the body itself must be one row.
	body := strings.TrimLeft(out, "\n")
	if strings.Count(body, "\n") > 0 {
		t.Fatalf("expected single-row body, got: %q", body)
	}
}

func TestSummarizeArgs_TruncatesLongInput(t *testing.T) {
	long := strings.Repeat("x", 200)
	got := summarizeArgs(map[string]any{"k": long}, argsSummaryCompact)
	// rune count is the meaningful display width; ellipsis is one rune.
	if n := len([]rune(got)); n > argsSummaryCompact {
		t.Fatalf("expected truncation to ≤%d runes, got %d", argsSummaryCompact, n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis suffix on truncated args: %q", got)
	}
}

func TestSummarizeArgs_Empty(t *testing.T) {
	if got := summarizeArgs(nil, argsSummaryCompact); got != "" {
		t.Fatalf("want empty string, got %q", got)
	}
}

func TestSummarizeArgs_SingleKeyDropsJSON(t *testing.T) {
	cases := []struct {
		in   map[string]any
		want string
	}{
		{map[string]any{"pattern": "needle"}, "pattern: needle"},
		{map[string]any{"path": "internal/tui"}, "path: internal/tui"},
		{map[string]any{"cmd": "go test"}, "cmd: go test"},
	}
	for _, c := range cases {
		if got := summarizeArgs(c.in, argsSummaryCompact); got != c.want {
			t.Fatalf("summarizeArgs(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSummarizeArgs_UnknownKeyKeepsJSON(t *testing.T) {
	got := summarizeArgs(map[string]any{"weird_key": "v"}, argsSummaryCompact)
	if !strings.HasPrefix(got, "{") {
		t.Fatalf("expected JSON fallback for unknown key, got %q", got)
	}
}

func TestShortenPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX absolute paths; filepath.IsAbs is false for them on Windows")
	}
	pathRootsOnce.Do(func() {})
	cachedCwd = "/Users/x/projects/bee"
	cachedHome = "/Users/x"
	defer func() { cachedCwd = ""; cachedHome = "" }()
	cases := []struct{ in, want string }{
		{"/Users/x/projects/bee/internal/tui/stream.go", "internal/tui/stream.go"},
		{"/Users/x/.config/foo", "~/.config/foo"},
		{"/etc/hosts", "/etc/hosts"},
		{"relative/path.go", "relative/path.go"},
		{"", ""},
	}
	for _, c := range cases {
		if got := shortenPath(c.in); got != c.want {
			t.Errorf("shortenPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRenderStreaming_EmptyShowsPlaceholder(t *testing.T) {
	// empty partial → loader animation row. swarm phase-0 emits braille
	// glyphs; no leading ⬢ since the animation alone signals activity.
	r := NewStreamRenderer(DefaultStyles(), 80)
	r.SetLoaderStyle(LoaderStyleSwarm)
	out := stripANSI(r.RenderStreaming("", 0))
	if strings.TrimSpace(out) == "" {
		t.Fatalf("expected non-empty loader row: %q", out)
	}
	if strings.Contains(out, "⬢") {
		t.Fatalf("loader row should not carry the role glyph: %q", out)
	}
}

func TestFormatAndParseInlineShell_Roundtrip(t *testing.T) {
	cases := []struct {
		cmd, out string
		isErr    bool
	}{
		{"ls", "a\nb\n", false},
		{"false", "", true},
		{"git status", "clean", false},
	}
	for _, c := range cases {
		s := formatInlineShell(c.cmd, c.out, c.isErr)
		cmd, out, isErr, ok := parseInlineShell(s)
		if !ok {
			t.Fatalf("parseInlineShell failed for %q", s)
		}
		if cmd != c.cmd || isErr != c.isErr {
			t.Fatalf("roundtrip: cmd=%q isErr=%v want %q/%v", cmd, isErr, c.cmd, c.isErr)
		}
		if strings.TrimRight(out, "\n") != strings.TrimRight(c.out, "\n") {
			t.Fatalf("roundtrip out: %q vs %q", out, c.out)
		}
	}
}

func TestParseInlineShell_RejectsPlain(t *testing.T) {
	for _, s := range []string{"hello", "$ ls", "[shell exit=0", "[shell exit=]\n"} {
		if _, _, _, ok := parseInlineShell(s); ok {
			t.Fatalf("expected reject for %q", s)
		}
	}
}

func TestRenderMessage_InlineShellStyled(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	payload := formatInlineShell("ls", "file1.go\nfile2.go", false)
	m := types.Message{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: payload}},
	}
	rendered := r.RenderMessage(m)
	plain := stripANSI(rendered)
	if !strings.Contains(plain, "$ ls") {
		t.Fatalf("missing `$ ls` in %q", plain)
	}
	for _, want := range []string{"file1.go", "file2.go"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in %q", want, plain)
		}
	}
	// glyph replaced by `$` prompt — neither role glyph should appear.
	for _, glyph := range []string{"▸", "⬢", "◇"} {
		if strings.Contains(plain, glyph) {
			t.Fatalf("unexpected role glyph %q in %q", glyph, plain)
		}
	}
	_ = rendered
}

func TestRenderMessage_InlineShellError(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	payload := formatInlineShell("false", "exit 1", true)
	m := types.Message{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: payload}},
	}
	plain := stripANSI(r.RenderMessage(m))
	if !strings.Contains(plain, "$ false") {
		t.Fatalf("missing cmd in %q", plain)
	}
	if !strings.Contains(plain, "exit 1") {
		t.Fatalf("missing output in %q", plain)
	}
}

// failing bash tool: tool-result preview should swap the bare "exit N" line
// for the originating command (so the user sees *what* failed, not just
// that something did). The exit code stays visible as a dim suffix.
func TestRenderToolResult_BashErrorSurfacesCmd(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	cmd := `grep -rn "needle" internal/`
	m := types.Message{
		Role: types.RoleAssistant,
		Content: []types.ContentBlock{
			{Type: types.BlockToolUse, Use: &types.ToolUse{
				ID:    "u1",
				Name:  "bash",
				Input: map[string]any{"command": cmd},
			}},
		},
	}
	// register the tool use, then render the matching tool result.
	_ = r.RenderMessage(m)
	res := types.Message{
		Role: types.RoleTool,
		Content: []types.ContentBlock{{Type: types.BlockToolResult, Result: &types.ToolResult{
			UseID:   "u1",
			Content: "exit 1\n",
			IsError: true,
		}}},
	}
	plain := stripANSI(r.RenderMessage(res))
	if !strings.Contains(plain, "$ "+cmd) {
		t.Fatalf("missing failed-cmd header in %q", plain)
	}
	if !strings.Contains(plain, "(exit 1)") {
		t.Fatalf("missing exit tag in %q", plain)
	}
}

// refused/denied bash tool: tool-result preview should swap the bare
// "refused …" line for the originating command rendered with a yellow-bg
// warn badge — distinguishes "blocked, not broken" from a real failure.
func TestRenderToolResult_RefusedSurfacesCmdYellow(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	cmd := `bash -c 'rm -rf /tmp/x'`
	m := types.Message{
		Role: types.RoleAssistant,
		Content: []types.ContentBlock{
			{Type: types.BlockToolUse, Use: &types.ToolUse{
				ID:    "u1",
				Name:  "bash",
				Input: map[string]any{"command": cmd},
			}},
		},
	}
	_ = r.RenderMessage(m)
	res := types.Message{
		Role: types.RoleTool,
		Content: []types.ContentBlock{{Type: types.BlockToolResult, Result: &types.ToolResult{
			UseID:   "u1",
			Content: "refused by user: nested shell -c invocation (shell-dash-c). Try a different approach.\n",
			IsError: true,
		}}},
	}
	plain := stripANSI(r.RenderMessage(res))
	if !strings.Contains(plain, "$ "+cmd) {
		t.Fatalf("missing refused-cmd header in %q", plain)
	}
	if !strings.Contains(plain, "refused by user") {
		t.Fatalf("missing refusal body line in %q", plain)
	}
}

func TestRenderToolUse_EditShowsDiffNotJSON(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	m := types.Message{
		Role: types.RoleAssistant,
		Content: []types.ContentBlock{{Type: types.BlockToolUse, Use: &types.ToolUse{
			ID:   "u1",
			Name: "edit",
			Input: map[string]any{
				"path": "README.md",
				"old":  "alpha",
				"new":  "beta",
			},
		}}},
	}
	out := stripANSI(r.RenderMessage(m))
	for _, want := range []string{"◇", "edit", "README.md", "- alpha", "+ beta"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
	// raw JSON shape must not leak into the card anymore.
	if strings.Contains(out, `{"new"`) || strings.Contains(out, `"old":`) {
		t.Fatalf("raw json leaked into edit card: %q", out)
	}
}

func TestRenderToolUse_ApplyPatchColorsHunkLines(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	patch := "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1,2 +1,2 @@\n-old\n+new\n"
	m := types.Message{
		Role: types.RoleAssistant,
		Content: []types.ContentBlock{{Type: types.BlockToolUse, Use: &types.ToolUse{
			ID: "u1", Name: "apply_patch", Input: map[string]any{"patch": patch},
		}}},
	}
	out := stripANSI(r.RenderMessage(m))
	for _, want := range []string{"apply_patch", "x.go", "-old", "+new", "@@"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestRenderToolUse_WriteListsContentLines(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	m := types.Message{
		Role: types.RoleAssistant,
		Content: []types.ContentBlock{{Type: types.BlockToolUse, Use: &types.ToolUse{
			ID: "u1", Name: "write", Input: map[string]any{
				"path":    "out.txt",
				"content": "one\ntwo",
			},
		}}},
	}
	out := stripANSI(r.RenderMessage(m))
	for _, want := range []string{"write", "out.txt", "+ one", "+ two", "2 lines"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestRenderToolUse_EditCompactCapsLongDiff(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString("n\n")
	}
	m := types.Message{
		Role: types.RoleAssistant,
		Content: []types.ContentBlock{{Type: types.BlockToolUse, Use: &types.ToolUse{
			ID: "u1", Name: "edit", Input: map[string]any{
				"path": "f.go", "old": "x", "new": b.String(),
			},
		}}},
	}
	out := stripANSI(r.RenderMessage(m))
	if !strings.Contains(out, "more") {
		t.Fatalf("compact diff should signal overflow: %q", out)
	}
}

func TestRenderLoader_CyclesAndDoesNotPanic(t *testing.T) {
	// pin swarm so phase transitions (frames 80, 240, …) bring in fresh
	// frame sets; non-swarm styles cycle a single small set.
	r := NewStreamRenderer(DefaultStyles(), 80)
	r.SetLoaderStyle(LoaderStyleSwarm)
	seen := map[string]bool{}
	for f := 0; f < 300; f++ {
		seen[stripANSI(r.RenderStreaming("", f))] = true
	}
	if len(seen) < 4 {
		t.Fatalf("loader animation too static: only %d distinct frames", len(seen))
	}
	// negative frames must not panic (modulus math defensive).
	_ = r.RenderStreaming("", -5)
}

// sweep every named painter across a frame range. checks no panics, each
// produces ≥2 distinct frames (i.e. actually animates), and output has a
// leading newline (gap above loader).
func TestRenderLoader_AllNamedPaintersAnimate(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	for style := LoaderStylePulse; style <= LoaderStyleMarathon; style++ {
		r.SetLoaderStyle(style)
		seen := map[string]bool{}
		for f := 0; f < 120; f++ {
			out := stripANSI(r.RenderStreaming("", f))
			if !strings.HasPrefix(out, "\n") {
				t.Errorf("style %d frame %d: missing leading newline above loader", style, f)
			}
			seen[out] = true
		}
		if len(seen) < 2 {
			t.Errorf("style %d animation too static across 120 frames: %d distinct", style, len(seen))
		}
	}
}

// loader-with-content path should NOT prepend the gap newline (gap is
// loader-only).
func TestRenderStreaming_PartialDoesNotPrependBlankLine(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	out := stripANSI(r.RenderStreaming("hello", 0))
	if strings.HasPrefix(out, "\n") {
		t.Fatalf("partial render should not start with blank line: %q", out)
	}
}
