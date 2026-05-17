package tui

import (
	"errors"

	"golang.design/x/clipboard"
)

// clipboardInitErr captures whether the OS clipboard backend is reachable.
// nil = ready; non-nil = unsupported environment (headless CI, no xclip/xsel,
// missing cocoa frameworks). ReadClipboardImage returns this error verbatim so
// callers can surface "clipboard unavailable" instead of crashing.
var clipboardInitErr = clipboard.Init()

// ReadClipboardImage returns the raw image bytes from the system clipboard or
// an error if no image is available (or the clipboard backend is missing).
func ReadClipboardImage() ([]byte, error) {
	if clipboardInitErr != nil {
		return nil, clipboardInitErr
	}
	b := clipboard.Read(clipboard.FmtImage)
	if len(b) == 0 {
		return nil, errors.New("no image in clipboard")
	}
	return b, nil
}
