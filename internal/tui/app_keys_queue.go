package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/types"
)

// handleImagePaste reads the system clipboard and stages an image for the
// next submit. Only fires when idle — staging mid-stream would race with the
// running engine's message slice. Failure (no image, headless clipboard)
// surfaces via the standard error line.
func (m Model) handleImagePaste() (tea.Model, tea.Cmd) {
	if m.state != StateIdle {
		return m, nil
	}
	img, err := ReadClipboardImage()
	if err != nil {
		return m.setError("clipboard: " + err.Error())
	}
	m.pendingImage = img
	m.messages = append(m.messages, ephemeral("(image staged: "+bytesHuman(len(img))+")"))
	return m, m.flush()
}

// handlePaste intercepts a bracketed paste. When the pasted text is (or
// contains) a local image file path, it stages the image(s) and inserts a
// compact "[Image: name]" label into the input instead of the raw path.
// Ordinary text pastes fall through to the textarea unchanged.
func (m Model) handlePaste(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pasted := string(msg.Runes)
	// common case: the whole paste is one dragged file (handles unescaped
	// spaces a single token-scan would split).
	if blk, base, ok := loadImage(unescape(pasted)); ok {
		m.pendingImages = append(m.pendingImages, blk)
		m.input.InsertString(imageLabel(base))
		return m, nil
	}
	// else: scan for image path tokens embedded in surrounding text.
	if clean, imgs := extractImagePaths(pasted); len(imgs) > 0 {
		m.pendingImages = append(m.pendingImages, imgs...)
		m.input.InsertString(clean)
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleFollowUp pushes the current input onto the follow-up queue. Fires
// regardless of state — queued runs kick off as the current turn finishes.
func (m Model) handleFollowUp() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	m.input.Reset()
	m.queue = append(m.queue, text)
	m.messages = append(m.messages, userEcho(text, "queued"))
	return m, m.flush()
}

// assistantText builds a plain assistant text message.
func assistantText(text string) types.Message {
	return types.Message{
		Role:    types.RoleAssistant,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: text}},
	}
}

// ephemeral builds a scrollback-only assistant note, never sent to the model.
func ephemeral(text string) types.Message {
	m := assistantText(text)
	m.Ephemeral = true
	return m
}

// scrollback echo of text you typed mid-turn: your turn's bar and panel, a
// quiet tag saying what happens to it. never sent to the model as typed
func userEcho(text, tag string) types.Message {
	return types.Message{
		Role:      types.RoleUser,
		Content:   []types.ContentBlock{{Type: types.BlockText, Text: text}},
		Display:   text + "  " + glyphSep + " " + tag,
		Ephemeral: true,
	}
}

// handleInterrupt interrupts the current turn and queues the new message as the
// next turn. A soft steer lets the model continue its old task after an explicit
// new instruction; starting a fresh turn makes the latest message take priority.
func (m Model) handleInterrupt() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	m.input.Reset()
	m.queue = append(m.queue, text)
	m.messages = append(m.messages, userEcho(text, "interrupting"))
	m.cancelTurn()
	m.interrupted = true
	return m, m.flush()
}
