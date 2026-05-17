package tui

import (
	"bytes"
	"io"
	"os"

	"github.com/charmbracelet/x/term"
)

// InstallModifyOtherKeys turns on xterm modifyOtherKeys level 1 on the
// provided writer (typically os.Stdout) and wraps os.Stdin in a translator
// that decodes the resulting CSI sequences into the bytes bubbletea's parser
// recognises. The returned io.Reader is suitable for tea.WithInput. The
// returned cleanup func writes the disable sequence — defer it before
// tea.Program.Run returns so the terminal is left in a sane state.
//
// When stdout is not a TTY the function is a no-op and returns os.Stdin
// unwrapped + a no-op cleanup; piped invocations stay clean.
func InstallModifyOtherKeys(out io.Writer) (io.Reader, func()) {
	noop := func() {}
	f, ok := out.(*os.File)
	if !ok || !term.IsTerminal(f.Fd()) {
		return os.Stdin, noop
	}
	if _, err := f.WriteString(modifyOtherKeysEnable); err != nil {
		return os.Stdin, noop
	}
	cleanup := func() { _, _ = f.WriteString(modifyOtherKeysDisable) }
	return newCSITranslator(os.Stdin, os.Stdin.Fd()), cleanup
}

// xterm modifyOtherKeys level 1 + Kitty keyboard "disambiguate" level.
// Two protocols, two terminal cohorts:
//
//   - xterm modifyOtherKeys (`CSI > 4 ; 1 m`): iTerm 3.4+, classic xterm,
//     older Wezterm. Reports chords like shift+enter as `CSI 27;2;13~`.
//   - Kitty keyboard protocol (`CSI > 1 u`): Ghostty (default since 1.0),
//     Kitty, foot, recent Wezterm. Reports as `CSI 13;2u`.
//
// Sending BOTH is harmless: a terminal that only speaks one quietly
// ignores the other. Plain keys (enter, tab, esc, ...) keep their classic
// bytes in both modes so bubbletea v1.3's parser stays happy. The
// translator below decodes either encoding back into bytes bubbletea
// understands natively.
const (
	modifyOtherKeysEnable  = "\x1b[>4;1m\x1b[>1u"
	modifyOtherKeysDisable = "\x1b[<u\x1b[>4m"
)

// csiTranslator wraps an io.ReadWriteCloser (typically os.Stdin via a
// term.File) and rewrites a tiny set of modifyOtherKeys CSI sequences
// into bytes bubbletea understands natively. Everything else passes
// through. Implements term.File so bubbletea still recognises the wrapped
// stdin and puts the real FD into raw mode.
//
// Currently rewritten:
//
//	shift+enter (CSI 27;2;13~) → '\n' (ctrl+j → InsertNewline)
type csiTranslator struct {
	src    io.ReadWriteCloser
	fd     uintptr
	carry  []byte // partial CSI bytes held over from prior Read
	output bytes.Buffer
}

func newCSITranslator(src io.ReadWriteCloser, fd uintptr) *csiTranslator {
	return &csiTranslator{src: src, fd: fd}
}

func (c *csiTranslator) Read(p []byte) (int, error) {
	if c.output.Len() > 0 {
		return c.output.Read(p)
	}
	tmp := make([]byte, len(p))
	n, err := c.src.Read(tmp)
	if n > 0 {
		buf := append(c.carry, tmp[:n]...)
		c.carry = nil
		translated, held := translateModifyOtherKeys(buf)
		c.carry = held
		c.output.Write(translated)
	}
	if c.output.Len() == 0 {
		return 0, err
	}
	rn, _ := c.output.Read(p)
	return rn, err
}

func (c *csiTranslator) Write(p []byte) (int, error) { return c.src.Write(p) }
func (c *csiTranslator) Close() error                { return c.src.Close() }
func (c *csiTranslator) Fd() uintptr                 { return c.fd }

// translateModifyOtherKeys scans buf for the shift+enter chord in both
// encodings modern terminals produce, then returns (translated, held).
// `held` is a tail slice that looks like an incomplete CSI sequence and
// must be re-prepended to the next Read so a chord split across reads is
// not missed. Unknown CSI sequences pass through unchanged so bubbletea
// sees them as it would natively.
//
// Encodings handled:
//
//   - xterm modifyOtherKeys (level 1/2): CSI 27;2;13~ — iTerm 3.4+,
//     classic xterm. Enabled by the CSI > 4 ; 1 m escape we emit at startup.
//   - Kitty keyboard protocol: CSI 13;2u (and the CSI 13;2;13u variant
//     terminals emit when "report associated text" is enabled — Wezterm
//     and Ghostty at higher protocol levels). Ghostty (default), Kitty,
//     foot, Wezterm with kitty_keyboard. These terminals ignore our
//     modifyOtherKeys enable seq and use their own protocol, so the
//     xterm-style encoding never arrives; without this translation
//     shift+enter falls through to bubbletea as an unknownCSISequenceMsg
//     and the user's newline silently disappears (or, on some terminals,
//     the bare \r leaks through and Submit fires instead).
func translateModifyOtherKeys(buf []byte) (out, held []byte) {
	out = bytes.ReplaceAll(buf, []byte("\x1b[27;2;13~"), []byte("\n"))
	out = bytes.ReplaceAll(out, []byte("\x1b[13;2u"), []byte("\n"))
	// associated-text variant: same chord, but the terminal appends the
	// codepoint of the produced text (13 = CR) so a higher Kitty protocol
	// level enabled by an outer program in the same session doesn't bypass
	// translation.
	out = bytes.ReplaceAll(out, []byte("\x1b[13;2;13u"), []byte("\n"))
	// Kitty disambiguate mode reports Esc as CSI 27 u (not 0x1b) so it can't
	// be confused with the start of a CSI introducer. Map back to legacy ESC
	// byte so panes that check km.String() == "esc" can see it.
	out = bytes.ReplaceAll(out, []byte("\x1b[27u"), []byte{0x1b})
	// Kitty CSI u for ctrl+letter chords (a..z, mod 5 = ctrl) → legacy 0x01..0x1a.
	// With disambiguate mode enabled (CSI > 1 u), Ghostty/Kitty/foot stop sending
	// the bare 0x03/0x04/... bytes for ctrl+letter and instead emit CSI 99;5u etc.
	// Bubbletea v1's parser does not recognise CSI u, so without this translation
	// ctrl+c and ctrl+d arrive as unknownCSISequenceMsg and the user is trapped.
	for c := byte('a'); c <= 'z'; c++ {
		seq := []byte{0x1b, '['}
		seq = append(seq, []byte(itoa(int(c)))...)
		seq = append(seq, ';', '5', 'u')
		out = bytes.ReplaceAll(out, seq, []byte{c - 'a' + 1})
	}
	// hold back a trailing partial ESC[ sequence (no terminator yet) so a
	// chord that arrives across two reads still matches on the next pass.
	if idx := bytes.LastIndexByte(out, 0x1b); idx >= 0 {
		tail := out[idx:]
		if isPartialCSI(tail) {
			held = append([]byte(nil), tail...)
			out = out[:idx]
		}
	}
	return out, held
}

// itoa renders n as ASCII decimal. Avoids strconv import in a hot path that
// only needs unsigned small ints (Unicode codepoints 97..122).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [4]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// isPartialCSI reports whether s looks like an incomplete CSI sequence —
// starts with ESC '[' and has no final byte yet. Used to hold input back
// across read boundaries. A bare ESC byte is NOT considered partial: in
// modern terminals (raw mode, ICANON off) the kernel delivers a full CSI
// burst in a single read, so a lone trailing ESC almost certainly is the
// user pressing Escape — holding it would mean the keystroke never reaches
// bubbletea until the next byte arrives.
func isPartialCSI(s []byte) bool {
	if len(s) < 2 || s[0] != 0x1b || s[1] != '[' {
		return false
	}
	// scan params/intermediates until a final byte (0x40-0x7e). If none, partial.
	for i := 2; i < len(s); i++ {
		b := s[i]
		if b >= 0x40 && b <= 0x7e {
			return false
		}
	}
	return true
}
