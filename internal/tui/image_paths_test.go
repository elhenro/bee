package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/types"
)

func TestHandlePaste_StagesImageAndShowsLabel(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Screenshot 2026-06-06 at 19.37.34.png")
	if err := os.WriteFile(p, []byte("PNGDATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	m.input.SetValue("what is this? ")
	m.input.CursorEnd()
	// drag-and-drop: terminal sends one bracketed paste with escaped spaces.
	escaped := strings.ReplaceAll(p, " ", `\ `)
	m2, _ := m.handlePaste(tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune(escaped)})
	m = m2.(Model)

	if len(m.pendingImages) != 1 {
		t.Fatalf("want 1 staged image, got %d", len(m.pendingImages))
	}
	if string(m.pendingImages[0].Data) != "PNGDATA" {
		t.Errorf("staged data = %q", m.pendingImages[0].Data)
	}
	val := m.input.Value()
	if strings.Contains(val, dir) || strings.Contains(val, ".png") {
		t.Errorf("input must not show raw path: %q", val)
	}
	if !strings.Contains(val, "[Image: ") {
		t.Errorf("input should show image label: %q", val)
	}
}

func TestHandlePaste_PlainTextFallsThrough(t *testing.T) {
	m := newTestModel(t)
	m2, _ := m.handlePaste(tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("just some text")})
	m = m2.(Model)
	if len(m.pendingImages) != 0 {
		t.Errorf("plain text must not stage images")
	}
	if !strings.Contains(m.input.Value(), "just some text") {
		t.Errorf("plain paste should land in input: %q", m.input.Value())
	}
}

func TestUnescape(t *testing.T) {
	if got := unescape(`a\ b\ c`); got != "a b c" {
		t.Errorf("unescape = %q", got)
	}
	if got := unescape("nobackslash"); got != "nobackslash" {
		t.Errorf("unescape passthrough = %q", got)
	}
}

func TestExtractImagePaths_DragEscapedPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Screenshot 2026-06-06 at 19.37.34.png")
	if err := os.WriteFile(p, []byte("PNGDATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	// drag-and-drop hands over spaces as "\ "
	escaped := strings.ReplaceAll(p, " ", `\ `)
	clean, imgs := extractImagePaths("what is this? " + escaped)

	if len(imgs) != 1 {
		t.Fatalf("want 1 image, got %d", len(imgs))
	}
	if imgs[0].Type != types.BlockImage || imgs[0].MediaType != "image/png" {
		t.Errorf("block = %+v", imgs[0])
	}
	if string(imgs[0].Data) != "PNGDATA" {
		t.Errorf("data = %q", imgs[0].Data)
	}
	if !strings.Contains(clean, "[Image: ") || strings.Contains(clean, dir) {
		t.Errorf("clean text should hide path, show label: %q", clean)
	}
	// label capped at 24 runes inside the brackets
	label := clean[strings.Index(clean, "[Image: ")+len("[Image: ") : strings.LastIndex(clean, "]")]
	if len([]rune(label)) > imageLabelMax {
		t.Errorf("label %q exceeds %d runes", label, imageLabelMax)
	}
}

func TestExtractImagePaths_NonImageUntouched(t *testing.T) {
	clean, imgs := extractImagePaths("just text and /etc/hosts and notes.txt")
	if len(imgs) != 0 {
		t.Fatalf("want 0 images, got %d", len(imgs))
	}
	if !strings.Contains(clean, "/etc/hosts") {
		t.Errorf("non-image tokens must survive: %q", clean)
	}
}

func TestExtractImagePaths_MissingFileUntouched(t *testing.T) {
	_, imgs := extractImagePaths("/nope/does-not-exist.png")
	if len(imgs) != 0 {
		t.Errorf("missing file must not become an image block")
	}
}
