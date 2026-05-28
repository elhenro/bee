package remote

import (
	"strings"

	"rsc.io/qr"
)

// quiet border width in modules around the code.
const qrQuiet = 2

// RenderQR encodes s as a QR code and renders it as a multi-line string of
// unicode block runes suitable for a terminal. Returns error if encoding fails.
func RenderQR(s string) (string, error) {
	c, err := qr.Encode(s, qr.M)
	if err != nil {
		return "", err
	}

	dim := c.Size + qrQuiet*2
	dark := func(x, y int) bool {
		mx, my := x-qrQuiet, y-qrQuiet
		if mx < 0 || my < 0 || mx >= c.Size || my >= c.Size {
			return false // quiet zone stays light
		}
		return c.Black(mx, my)
	}

	var b strings.Builder
	// two vertical modules per text row via half-block runes
	for y := 0; y < dim; y += 2 {
		for x := 0; x < dim; x++ {
			top := dark(x, y)
			bot := y+1 < dim && dark(x, y+1)
			switch {
			case top && bot:
				b.WriteRune('█')
			case top:
				b.WriteRune('▀')
			case bot:
				b.WriteRune('▄')
			default:
				b.WriteRune(' ')
			}
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}
