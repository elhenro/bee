package tui

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

var fakePNG = append(append([]byte{}, pngMagic...), "payload"...)

// stubClip fakes the platform and the set of installed tools; run maps a tool
// name to its stdout (a missing key makes the tool fail). It returns the names
// of the tools that ran, in order.
func stubClip(t *testing.T, goos string, installed []string, run map[string][]byte) *[]string {
	t.Helper()
	oldOS, oldLook, oldRun := clipGOOS, clipLookPath, clipRun
	t.Cleanup(func() { clipGOOS, clipLookPath, clipRun = oldOS, oldLook, oldRun })
	clipGOOS = goos
	clipLookPath = func(n string) (string, error) {
		for _, i := range installed {
			if i == n {
				return "/bin/" + n, nil
			}
		}
		return "", errors.New("not found")
	}
	var ran []string
	clipRun = func(name string, _ ...string) ([]byte, error) {
		ran = append(ran, name)
		out, ok := run[name]
		if !ok {
			return nil, errors.New("exit status 1")
		}
		return out, nil
	}
	return &ran
}

func TestReadClipboardImage_DarwinPrefersPngpaste(t *testing.T) {
	ran := stubClip(t, "darwin", []string{"pngpaste", "osascript"}, map[string][]byte{"pngpaste": fakePNG})
	got, err := ReadClipboardImage()
	if err != nil || string(got) != string(fakePNG) {
		t.Fatalf("got %q, %v", got, err)
	}
	if len(*ran) != 1 || (*ran)[0] != "pngpaste" {
		t.Errorf("ran %v, want only pngpaste", *ran)
	}
}

func TestReadClipboardImage_DarwinOsascriptFallback(t *testing.T) {
	osa := "«data PNGf" + strings.ToUpper(hex.EncodeToString(fakePNG)) + "»\n"
	stubClip(t, "darwin", []string{"osascript"}, map[string][]byte{"osascript": []byte(osa)})
	got, err := ReadClipboardImage()
	if err != nil || string(got) != string(fakePNG) {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestReadClipboardImage_LinuxToolOrder(t *testing.T) {
	both := []string{"wl-paste", "xclip"}
	run := map[string][]byte{"xclip": fakePNG}

	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	ran := stubClip(t, "linux", both, run)
	if _, err := ReadClipboardImage(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"wl-paste", "xclip"}; strings.Join(*ran, ",") != strings.Join(want, ",") {
		t.Errorf("wayland ran %v, want %v", *ran, want)
	}

	t.Setenv("WAYLAND_DISPLAY", "")
	ran = stubClip(t, "linux", both, run)
	if _, err := ReadClipboardImage(); err != nil {
		t.Fatal(err)
	}
	if len(*ran) != 1 || (*ran)[0] != "xclip" {
		t.Errorf("x11 ran %v, want only xclip", *ran)
	}
}

func TestReadClipboardImage_WindowsBase64(t *testing.T) {
	enc := base64.StdEncoding.EncodeToString(fakePNG) + "\r\n"
	stubClip(t, "windows", []string{"powershell"}, map[string][]byte{"powershell": []byte(enc)})
	got, err := ReadClipboardImage()
	if err != nil || string(got) != string(fakePNG) {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestReadClipboardImage_NoToolNamesTheFix(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	stubClip(t, "linux", nil, nil)
	_, err := ReadClipboardImage()
	if err == nil || !strings.Contains(err.Error(), "xclip") {
		t.Fatalf("want an install hint naming xclip, got %v", err)
	}
}

func TestReadClipboardImage_NoImage(t *testing.T) {
	cases := map[string]map[string][]byte{
		"tool fails":     {},
		"empty stdout":   {"xclip": nil},
		"not a png":      {"xclip": []byte("GIF89a")},
		"osascript text": {"osascript": []byte("hello")},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("WAYLAND_DISPLAY", "")
			goos := "linux"
			if _, osa := run["osascript"]; osa {
				goos = "darwin"
			}
			stubClip(t, goos, []string{"xclip", "osascript"}, run)
			got, err := ReadClipboardImage()
			if !errors.Is(err, errNoClipImage) || got != nil {
				t.Fatalf("got %q, %v; want errNoClipImage", got, err)
			}
		})
	}
}

// the real backend must return, never panic, and never succeed with zero bytes.
func TestReadClipboardImage_RealBackendSmoke(t *testing.T) {
	b, err := ReadClipboardImage()
	if err == nil && len(b) == 0 {
		t.Fatal("nil error but no bytes")
	}
	if err != nil && err.Error() == "" {
		t.Fatal("error must have a message")
	}
}
