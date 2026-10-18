package ui

import (
	"image"
	"image/color"
	"image/draw"

	"github.com/ventz/falldown/internal/calc"
)

// The key strip above the LCD: keycaps in the panel's colors with labels in
// the calculator's small font, so the controls read as part of the game.
// It wraps onto more rows in a narrow window, and H hides it.

type helpItem struct {
	keys  []string
	label string
}

var helpItems = []helpItem{
	{[]string{"<", ">"}, "steer"},
	{[]string{"Space"}, "start"},
	{[]string{"Tab"}, "speed"},
	{[]string{"M"}, "pause"},
	{[]string{"O"}, "resume"},
	{[]string{"Esc"}, "quit"},
	{[]string{"F11"}, "full screen"},
	{[]string{"H"}, "hide"},
}

// arrows replace the font's < and >, which read as less-than and
// greater-than rather than arrow keys.
var arrows = map[rune][]string{
	'<': {"..#..", ".#...", "#####", ".#...", "..#.."},
	'>': {"..#..", "...#.", "#####", "...#.", "..#.."},
}

var (
	keyFace   = color.RGBA{0x9C, 0xA8, 0x8A, 0xFF} // the LCD glass
	keyTop    = color.RGBA{0xC3, 0xCF, 0xAE, 0xFF}
	keyShadow = color.RGBA{0x6F, 0x7A, 0x5F, 0xFF}
	keyInk    = color.RGBA{0x1C, 0x22, 0x1A, 0xFF} // a lit LCD pixel
	labelInk  = color.RGBA{0xE3, 0xE7, 0xDC, 0xFF}
)

// Sizes in font pixels.
const (
	keyPadX  = 2
	keyH     = 1 + 5 + 1 + 1 // padding, glyph, padding, shadow
	rowH     = keyH + 3      // a key plus the gap to the next row
	keyGap   = 2             // between the keys of one item
	labelGap = 3             // between the keys and their label
	itemGap  = 9             // between items
)

func glyph(r rune) []string {
	if g, ok := arrows[r]; ok {
		return g
	}
	return calc.SmallGlyph(r)
}

func textWidth(s string) int {
	w := 0
	for i, r := range s {
		if i > 0 {
			w++
		}
		w += len(glyph(r)[0])
	}
	return w
}

func keyWidth(k string) int { return textWidth(k) + 2*keyPadX }

func itemWidth(it helpItem) int {
	w := 0
	for i, k := range it.keys {
		if i > 0 {
			w += keyGap
		}
		w += keyWidth(k)
	}
	return w + labelGap + textWidth(it.label)
}

// helpRows flows the items into rows no wider than maxW font pixels.
func helpRows(maxW int) [][]helpItem {
	var rows [][]helpItem
	var row []helpItem
	w := 0
	for _, it := range helpItems {
		iw := itemWidth(it)
		if len(row) > 0 && w+itemGap+iw > maxW {
			rows = append(rows, row)
			row, w = nil, 0
		}
		if len(row) > 0 {
			w += itemGap
		}
		row = append(row, it)
		w += iw
	}
	return append(rows, row)
}

func rowWidth(row []helpItem) int {
	w := 0
	for i, it := range row {
		if i > 0 {
			w += itemGap
		}
		w += itemWidth(it)
	}
	return w
}

// helpStrip renders the strip for a screen width (in device pixels), with
// each font pixel drawn as px x px.
func helpStrip(screenW, px int) *image.RGBA {
	maxW := screenW/px - 2*itemGap
	rows := helpRows(maxW)
	img := image.NewRGBA(image.Rect(0, 0, screenW, len(rows)*rowH*px))
	draw.Draw(img, img.Bounds(), &image.Uniform{bezel}, image.Point{}, draw.Src)

	rect := func(x, y, w, h int, c color.RGBA) {
		draw.Draw(img, image.Rect(x*px, y*px, (x+w)*px, (y+h)*px), &image.Uniform{c}, image.Point{}, draw.Src)
	}
	text := func(x, y int, s string, c color.RGBA) {
		for i, r := range s {
			if i > 0 {
				x++
			}
			g := glyph(r)
			for gy, line := range g {
				for gx, ch := range line {
					if ch == '#' {
						rect(x+gx, y+gy, 1, 1, c)
					}
				}
			}
			x += len(g[0])
		}
	}

	for ri, row := range rows {
		x := (screenW/px - rowWidth(row)) / 2
		y := ri*rowH + 1
		for i, it := range row {
			if i > 0 {
				x += itemGap
			}
			for ki, k := range it.keys {
				if ki > 0 {
					x += keyGap
				}
				w := keyWidth(k)
				rect(x, y, w, keyH, keyFace)
				rect(x, y, w, 1, keyTop)
				rect(x, y+keyH-1, w, 1, keyShadow)
				text(x+keyPadX, y+1, k, keyInk)
				x += w
			}
			x += labelGap
			text(x, y+1, it.label, labelInk)
			x += textWidth(it.label)
		}
	}
	return img
}

// helpPixel picks the font-pixel size: 2 logical pixels, or 3 in a wide
// window, times the display's scale factor.
func helpPixel(screenW int, deviceScale float64) int {
	logical := 2
	if float64(screenW)/deviceScale >= 1600 {
		logical = 3
	}
	return max(1, int(float64(logical)*deviceScale+0.5))
}
