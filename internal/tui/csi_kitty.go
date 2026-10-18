package tui

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf8"
)

// kitty modifier bits (wire value is 1 + bitmask)
const (
	kittyShift = 1
	kittyAlt   = 2
	kittyCtrl  = 4
	kittyLocks = 64 | 128 // caps/num lock: not part of the chord
)

// translateKittyCSIu rewrites any remaining `CSI cp ; mod [; text] u` sequence
// into the legacy bytes bubbletea's parser knows: alt+key becomes ESC+key,
// ctrl+symbol becomes its C0 byte. Sequences it cannot represent (super/meta
// chords, functional keys, shifted symbols without text) pass through.
func translateKittyCSIu(buf []byte) []byte {
	var out bytes.Buffer
	for len(buf) > 0 {
		i := bytes.Index(buf, []byte("\x1b["))
		if i < 0 {
			out.Write(buf)
			break
		}
		out.Write(buf[:i])
		buf = buf[i:]
		end := csiEnd(buf)
		if end < 0 || buf[end] != 'u' {
			out.Write(buf[:2])
			buf = buf[2:]
			continue
		}
		if b, ok := kittyLegacy(string(buf[2:end])); ok {
			out.Write(b)
		} else {
			out.Write(buf[:end+1])
		}
		buf = buf[end+1:]
	}
	return out.Bytes()
}

// csiEnd returns the index of the final byte of the CSI at buf[0:], or -1.
func csiEnd(buf []byte) int {
	for i := 2; i < len(buf); i++ {
		if buf[i] >= 0x40 && buf[i] <= 0x7e {
			return i
		}
		if (buf[i] < '0' || buf[i] > '9') && buf[i] != ';' && buf[i] != ':' {
			return -1
		}
	}
	return -1
}

// kittyLegacy decodes the params of one CSI u sequence.
func kittyLegacy(params string) ([]byte, bool) {
	fields := strings.Split(params, ";")
	cp, ok := firstInt(fields[0])
	if !ok {
		return nil, false
	}
	mod := 1
	if len(fields) > 1 && fields[1] != "" {
		if mod, ok = firstInt(fields[1]); !ok || mod < 1 {
			return nil, false
		}
	}
	bits := (mod - 1) &^ kittyLocks
	if bits&^(kittyShift|kittyAlt|kittyCtrl) != 0 {
		return nil, false // super/hyper/meta: no legacy form
	}
	shift, alt, ctrl := bits&kittyShift != 0, bits&kittyAlt != 0, bits&kittyCtrl != 0

	var b []byte
	switch {
	case ctrl && shift:
		return nil, false // legacy bytes cannot tell ctrl+shift+x from ctrl+x
	case ctrl:
		c, ok := ctrlByte(cp)
		if !ok {
			return nil, false
		}
		b = []byte{c}
	case cp == 13 || cp == 9 || cp == 127:
		b = []byte{byte(cp)}
	case cp >= 32 && cp < 0x10ffff && !(cp >= 0xe000 && cp <= 0xf8ff) && utf8.ValidRune(rune(cp)):
		r := rune(cp)
		if shift {
			switch {
			case r >= 'a' && r <= 'z':
				r -= 'a' - 'A'
			case len(fields) > 2 && fields[2] != "":
				// associated text carries the shifted character
				t, ok := firstInt(fields[2])
				if !ok {
					return nil, false
				}
				r = rune(t)
			default:
				return nil, false
			}
		}
		b = utf8.AppendRune(nil, r)
	default:
		return nil, false
	}
	if alt {
		b = append([]byte{0x1b}, b...)
	}
	return b, true
}

// ctrlByte maps a key under ctrl to its C0 control byte.
func ctrlByte(cp int) (byte, bool) {
	switch {
	case cp >= 'a' && cp <= 'z':
		return byte(cp - 'a' + 1), true
	case cp == ' ' || cp == '2' || cp == '@':
		return 0x00, true
	case cp == '[' || cp == '3':
		return 0x1b, true
	case cp == '\\' || cp == '4':
		return 0x1c, true
	case cp == ']' || cp == '5':
		return 0x1d, true
	case cp == '^' || cp == '6':
		return 0x1e, true
	case cp == '_' || cp == '/' || cp == '7':
		return 0x1f, true
	case cp == '8' || cp == '?':
		return 0x7f, true
	}
	return 0, false
}

// firstInt parses the leading number of a `n[:sub...]` param.
func firstInt(s string) (int, bool) {
	head, _, _ := strings.Cut(s, ":")
	n, err := strconv.Atoi(head)
	return n, err == nil
}
