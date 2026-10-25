package tui

import (
	"errors"
	"sync"

	"golang.design/x/clipboard"
)

// clipboardInit reports whether the OS clipboard backend is reachable (nil =
// ready; non-nil = unsupported environment: headless CI, no xclip/xsel,
// missing cocoa frameworks). It runs on the first paste, not at process
// start, so runs that never paste (headless `bee run`) don't pay for it.
// ReadClipboardImage returns the error verbatim so callers can surface
// "clipboard unavailable" instead of crashing.
var clipboardInit = sync.OnceValue(clipboard.Init)

// ReadClipboardImage returns the raw image bytes from the system clipboard or
// an error if no image is available (or the clipboard backend is missing).
func ReadClipboardImage() ([]byte, error) {
	if err := clipboardInit(); err != nil {
		return nil, err
	}
	b := clipboard.Read(clipboard.FmtImage)
	if len(b) == 0 {
		return nil, errors.New("no image in clipboard")
	}
	return b, nil
}
