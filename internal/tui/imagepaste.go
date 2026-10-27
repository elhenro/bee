package tui

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// clipboard access shells out to the platform's paste tool: no CGo, so it works
// in the static release binaries. The seams are vars so tests can stub them.
var (
	clipGOOS     = runtime.GOOS
	clipLookPath = exec.LookPath
	clipRun      = runClipCmd
)

const clipTimeout = 10 * time.Second

var (
	errNoClipImage = errors.New("no image in clipboard")
	pngMagic       = []byte("\x89PNG\r\n\x1a\n")
	// osascript prints a PNG clipboard as «data PNGf<hex>»; the guillemets can
	// come out in either encoding depending on locale, so match the hex only
	osaPNG = regexp.MustCompile(`data PNGf([0-9A-Fa-f]+)`)
)

// clipCmd is one way to read the clipboard as PNG bytes on stdout.
type clipCmd struct {
	name   string
	args   []string
	decode func([]byte) ([]byte, error) // nil = stdout is already PNG
}

const winPasteScript = `Add-Type -AssemblyName System.Drawing; ` +
	`$i = Get-Clipboard -Format Image; if ($null -eq $i) { exit 1 }; ` +
	`$m = New-Object System.IO.MemoryStream; ` +
	`$i.Save($m, [System.Drawing.Imaging.ImageFormat]::Png); ` +
	`[Convert]::ToBase64String($m.ToArray())`

// clipCommands lists the paste tools present on this machine, best first, and
// a hint naming what to install when none is.
func clipCommands() ([]clipCmd, string) {
	has := func(n string) bool { _, err := clipLookPath(n); return err == nil }
	var cmds []clipCmd
	var hint string
	switch clipGOOS {
	case "darwin":
		hint = "install pngpaste"
		if has("pngpaste") {
			cmds = append(cmds, clipCmd{name: "pngpaste", args: []string{"-"}})
		}
		if has("osascript") {
			cmds = append(cmds, clipCmd{"osascript", []string{"-e", "the clipboard as «class PNGf»"}, decodeOsaPNG})
		}
	case "windows":
		hint = "powershell not found"
		if has("powershell") {
			cmds = append(cmds, clipCmd{"powershell", []string{"-NoProfile", "-Command", winPasteScript}, decodeBase64})
		}
	default:
		hint = "install wl-clipboard or xclip"
		if os.Getenv("WAYLAND_DISPLAY") != "" && has("wl-paste") {
			cmds = append(cmds, clipCmd{name: "wl-paste", args: []string{"--type", "image/png"}})
		}
		if has("xclip") {
			cmds = append(cmds, clipCmd{name: "xclip", args: []string{"-selection", "clipboard", "-t", "image/png", "-o"}})
		}
	}
	return cmds, hint
}

func runClipCmd(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), clipTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

func decodeOsaPNG(out []byte) ([]byte, error) {
	m := osaPNG.FindSubmatch(out)
	if m == nil {
		return nil, errNoClipImage
	}
	return hex.DecodeString(string(m[1]))
}

func decodeBase64(out []byte) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
}

// ReadClipboardImage returns the PNG bytes of the image on the system
// clipboard, or an error when there is none or no paste tool is installed.
func ReadClipboardImage() ([]byte, error) {
	cmds, hint := clipCommands()
	if len(cmds) == 0 {
		return nil, errors.New("no clipboard tool found: " + hint)
	}
	for _, c := range cmds {
		out, err := clipRun(c.name, c.args...)
		if err == nil && c.decode != nil {
			out, err = c.decode(out)
		}
		if err == nil && bytes.HasPrefix(out, pngMagic) {
			return out, nil
		}
	}
	return nil, errNoClipImage
}
