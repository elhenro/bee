package tui

import (
	"bytes"
	"io"
	"testing"
)

// fakeStdin is an io.ReadWriteCloser fed from a byte slice — feeds bytes in
// configurable-size chunks so we can verify split-read handling.
type fakeStdin struct {
	chunks [][]byte
}

func (f *fakeStdin) Read(p []byte) (int, error) {
	if len(f.chunks) == 0 {
		return 0, io.EOF
	}
	c := f.chunks[0]
	n := copy(p, c)
	if n == len(c) {
		f.chunks = f.chunks[1:]
	} else {
		f.chunks[0] = c[n:]
	}
	return n, nil
}
func (f *fakeStdin) Write(p []byte) (int, error) { return len(p), nil }
func (f *fakeStdin) Close() error                { return nil }

func drain(t *testing.T, r io.Reader) []byte {
	t.Helper()
	var out bytes.Buffer
	buf := make([]byte, 64)
	for {
		n, err := r.Read(buf)
		out.Write(buf[:n])
		if err == io.EOF {
			return out.Bytes()
		}
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if n == 0 {
			return out.Bytes()
		}
	}
}

func TestCSITranslator_ShiftEnterSinglePacket(t *testing.T) {
	f := &fakeStdin{chunks: [][]byte{[]byte("hi\x1b[27;2;13~bye")}}
	tr := newCSITranslator(f, 0)
	got := drain(t, tr)
	want := []byte("hi\nbye")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCSITranslator_ShiftEnterSplitAcrossReads(t *testing.T) {
	// chord split mid-CSI must still translate.
	f := &fakeStdin{chunks: [][]byte{
		[]byte("hi\x1b[27;2;"),
		[]byte("13~bye"),
	}}
	tr := newCSITranslator(f, 0)
	got := drain(t, tr)
	want := []byte("hi\nbye")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCSITranslator_PassesThroughPlainEnter(t *testing.T) {
	f := &fakeStdin{chunks: [][]byte{[]byte("hi\rbye")}}
	tr := newCSITranslator(f, 0)
	got := drain(t, tr)
	want := []byte("hi\rbye")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCSITranslator_PassesThroughUnknownCSI(t *testing.T) {
	// arrow-up `\x1b[A` is a complete CSI we don't translate.
	f := &fakeStdin{chunks: [][]byte{[]byte("\x1b[Aok")}}
	tr := newCSITranslator(f, 0)
	got := drain(t, tr)
	want := []byte("\x1b[Aok")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

// Kitty keyboard protocol reports shift+enter as `CSI 13;2u` — Ghostty
// (default since 1.0), Kitty, foot, and recent Wezterm emit this and
// ignore xterm modifyOtherKeys, so the older `CSI 27;2;13~` encoding
// never arrives. Translating to bare \n keeps the textarea's
// InsertNewline binding firing on shift+enter; without it the bytes
// reach bubbletea as an unknownCSISequenceMsg and the user's multi-line
// input silently collapses to its first line.
func TestCSITranslator_KittyShiftEnterSinglePacket(t *testing.T) {
	f := &fakeStdin{chunks: [][]byte{[]byte("hi\x1b[13;2ubye")}}
	tr := newCSITranslator(f, 0)
	got := drain(t, tr)
	want := []byte("hi\nbye")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCSITranslator_KittyShiftEnterSplitAcrossReads(t *testing.T) {
	f := &fakeStdin{chunks: [][]byte{
		[]byte("hi\x1b[13;"),
		[]byte("2ubye"),
	}}
	tr := newCSITranslator(f, 0)
	got := drain(t, tr)
	want := []byte("hi\nbye")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCSITranslator_BareEscPassesThrough(t *testing.T) {
	// regression: a lone ESC byte (user pressing Escape) must reach the
	// reader on this Read call — not get held back as a "partial CSI" until
	// the next byte arrives, which in practice may be never.
	f := &fakeStdin{chunks: [][]byte{[]byte("\x1b")}}
	tr := newCSITranslator(f, 0)
	got := drain(t, tr)
	want := []byte("\x1b")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

// regression: Kitty disambiguate mode reports plain Esc as CSI 27 u instead
// of the legacy 0x1b byte, so panes checking km.String() == "esc" never
// close. Translator must map it back.
func TestCSITranslator_KittyEscToLegacyByte(t *testing.T) {
	f := &fakeStdin{chunks: [][]byte{[]byte("\x1b[27u")}}
	tr := newCSITranslator(f, 0)
	got := drain(t, tr)
	want := []byte{0x1b}
	if !bytes.Equal(got, want) {
		t.Fatalf("got % x want % x", got, want)
	}
}

// regression: Ghostty/Kitty/foot with disambiguate (CSI > 1 u) report
// ctrl+letter as CSI N;5u instead of the legacy 0x01..0x1a byte. Without
// translation, ctrl+c and ctrl+d never reach the quit gate and the user
// is trapped in the TUI.
func TestCSITranslator_KittyCtrlLetterToLegacyByte(t *testing.T) {
	cases := map[string]byte{
		"\x1b[99;5u":  0x03, // ctrl+c
		"\x1b[100;5u": 0x04, // ctrl+d
		"\x1b[97;5u":  0x01, // ctrl+a
		"\x1b[122;5u": 0x1a, // ctrl+z
	}
	for in, want := range cases {
		f := &fakeStdin{chunks: [][]byte{[]byte(in)}}
		tr := newCSITranslator(f, 0)
		got := drain(t, tr)
		if len(got) != 1 || got[0] != want {
			t.Errorf("%q: got % x want %02x", in, got, want)
		}
	}
}
