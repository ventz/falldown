// Package lcdview turns the emulated 96x64 LCD into pixels that look like
// the real panel: grey-green glass, a faint pixel grid, and optional
// ghosting from the slow liquid crystal.
package lcdview

import (
	"image"
	"image/color"
	"math"
	"time"

	"github.com/ventz/falldown/internal/calc"
)

var (
	glass = rgb(0x9C, 0xA8, 0x8A) // panel background
	unlit = rgb(0x95, 0xA1, 0x83) // a pixel that is off, slightly visible
	lit   = rgb(0x1C, 0x22, 0x1A) // a pixel that is on
)

func rgb(r, g, b uint8) [3]float32 { return [3]float32{float32(r), float32(g), float32(b)} }

// View holds per-pixel darkness so the display can fade like an LCD.
type View struct {
	// Ghost is the liquid crystal's response time constant. Zero means
	// pixels switch instantly.
	Ghost time.Duration

	level [calc.Width * calc.Height]float32
	on    bool
	last  time.Time
}

// New returns a view with the given ghosting time constant.
func New(ghost time.Duration) *View { return &View{Ghost: ghost, on: true} }

// Update moves every pixel toward the LCD contents.
func (v *View) Update(buf calc.Buffer, on bool, now time.Time) {
	alpha := float32(1)
	if v.Ghost > 0 && !v.last.IsZero() {
		dt := now.Sub(v.last)
		if dt > 200*time.Millisecond {
			dt = 200 * time.Millisecond
		}
		alpha = float32(1 - math.Exp(-float64(dt)/float64(v.Ghost)))
	}
	v.last = now
	v.on = on
	for y := range calc.Height {
		for x := range calc.Width {
			target := float32(0)
			if on && buf.Pixel(x, y) {
				target = 1
			}
			i := y*calc.Width + x
			v.level[i] += (target - v.level[i]) * alpha
		}
	}
}

// Render draws the panel at scale pixels per LCD pixel into an RGBA buffer
// of (96*scale) x (64*scale).
func (v *View) Render(pix []byte, scale int) {
	gap := scale / 7 // the thin grid between pixels
	w := calc.Width * scale
	for py := range calc.Height * scale {
		y, iy := py/scale, py%scale
		row := pix[py*w*4 : (py+1)*w*4]
		for px := range w {
			x, ix := px/scale, px%scale
			c := glass
			if v.on && ix >= gap && iy >= gap {
				l := v.level[y*calc.Width+x]
				for k := range 3 {
					c[k] = unlit[k] + (lit[k]-unlit[k])*l
				}
			}
			o := px * 4
			row[o], row[o+1], row[o+2], row[o+3] = uint8(c[0]), uint8(c[1]), uint8(c[2]), 0xFF
		}
	}
}

// RenderSmall writes one RGBA pixel per LCD pixel (96x64), with no grid.
// Scale it up with nearest-neighbor filtering and lay GridMask on top.
func (v *View) RenderSmall(pix []byte) {
	for i, l := range v.level {
		c := glass
		if v.on {
			for k := range 3 {
				c[k] = unlit[k] + (lit[k]-unlit[k])*l
			}
		}
		o := i * 4
		pix[o], pix[o+1], pix[o+2], pix[o+3] = uint8(c[0]), uint8(c[1]), uint8(c[2]), 0xFF
	}
}

// GridMask returns an RGBA overlay of (96*scale) x (64*scale) that paints
// the thin glass-colored grid between pixels and is transparent elsewhere.
func GridMask(scale int) []byte {
	gap := scale / 7
	w, h := calc.Width*scale, calc.Height*scale
	pix := make([]byte, w*h*4)
	if gap == 0 {
		return pix
	}
	for py := range h {
		for px := range w {
			if px%scale < gap || py%scale < gap {
				o := (py*w + px) * 4
				pix[o], pix[o+1], pix[o+2], pix[o+3] = uint8(glass[0]), uint8(glass[1]), uint8(glass[2]), 0xFF
			}
		}
	}
	return pix
}

// Image renders a snapshot of buf as an image, for screenshots.
func Image(buf calc.Buffer, on bool, scale int) *image.RGBA {
	v := New(0)
	v.Update(buf, on, time.Now())
	img := image.NewRGBA(image.Rect(0, 0, calc.Width*scale, calc.Height*scale))
	v.Render(img.Pix, scale)
	return img
}

// Glass is the panel color, for drawing a matching border.
func Glass() color.RGBA {
	return color.RGBA{uint8(glass[0]), uint8(glass[1]), uint8(glass[2]), 0xFF}
}
