package tui

import "strings"

// Sleeping-bee ASCII art. Three rows of fixed visual width so render layout
// doesn't bounce as frames cycle. Each frame is a [3]string. The left half
// holds drifting Z's, the right side holds a stable bee silhouette with a
// breathing antenna + eye wobble so it looks alive without flailing.

type beeFrame [3]string

// frames cycle on a slow tick (~600ms). Eight phases — drift Z's right, swap
// eye glyphs, settle. Tuned so the bee feels "asleep & content" rather than
// busy.
var beeFrames = []beeFrame{
	{
		`   z  Z  z    .       `,
		`        Z  .          ζ(-‿-)ζ   ~zzz~`,
		`                       ‾‾‾‾‾‾   `,
	},
	{
		`     z  Z  z    .     `,
		`          Z  .        ζ(-.-)ζ   ~zZz~`,
		`                       ‾‾‾‾‾‾   `,
	},
	{
		`       z  Z  z    .   `,
		`            Z  .      ζ(-‿-)ζ   ~Zzz~`,
		`                       ‾‾‾‾‾‾   `,
	},
	{
		`         z  Z  z    . `,
		`              Z  .    ζ(-.-)ζ   ~zZZ~`,
		`                       ‾‾‾‾‾‾   `,
	},
	{
		`           z  Z  z    `,
		`        .       Z  .  ζ(-‿-)ζ   ~ZZz~`,
		`                       ‾‾‾‾‾‾   `,
	},
	{
		`             z  Z  z  `,
		`     .  .       Z     ζ(-.-)ζ   ~Zzz~`,
		`                       ‾‾‾‾‾‾   `,
	},
	{
		`               z  Z  z`,
		`  z  .  .       Z     ζ(-‿-)ζ   ~zZz~`,
		`                       ‾‾‾‾‾‾   `,
	},
	{
		`  z              z  Z `,
		`     z  .  .          ζ(-.-)ζ   ~zzz~`,
		`                       ‾‾‾‾‾‾   `,
	},
}

// BeeFrame returns the three lines for the given tick. Pure function so
// renderers can call it without locking.
func BeeFrame(tick int) (top, mid, bot string) {
	f := beeFrames[((tick%len(beeFrames))+len(beeFrames))%len(beeFrames)]
	return f[0], f[1], f[2]
}

// BeeWidth returns the rendered width of a frame line (all frames padded to
// match). Caller uses it to right-align trailing content.
func BeeWidth() int {
	w := 0
	for _, f := range beeFrames {
		for _, line := range f {
			if n := len([]rune(strings.TrimRight(line, " "))); n > w {
				w = n
			}
		}
	}
	return w
}
