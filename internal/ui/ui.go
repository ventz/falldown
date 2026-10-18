// Package ui is the desktop window: it feeds the keyboard to the emulated
// keypad and paints the emulated LCD.
package ui

import (
	"image/color"
	"runtime"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/ventz/falldown/internal/calc"
	"github.com/ventz/falldown/internal/lcdview"
)

// TPS is how often input is sampled. The game reads the arrow keys once per
// frame (60 Hz at speed 1), so sampling at 240 Hz keeps the window from
// adding up to a frame of lag on top of the calculator's own.
const TPS = 240

// KeyMap lists the PC keys for each calculator key.
var KeyMap = map[byte][]ebiten.Key{
	calc.SkLeft:  {ebiten.KeyArrowLeft},
	calc.SkRight: {ebiten.KeyArrowRight},
	calc.SkUp:    {ebiten.KeyArrowUp},
	calc.SkDown:  {ebiten.KeyArrowDown},
	calc.Sk2nd:   {ebiten.KeySpace, ebiten.KeyEnter, ebiten.KeyNumpadEnter, ebiten.KeyZ},
	calc.SkAlpha: {ebiten.KeyA, ebiten.KeyTab},
	calc.SkMode:  {ebiten.KeyM},
	calc.SkClear: {ebiten.KeyEscape, ebiten.KeyBackspace, ebiten.KeyQ},
}

// OnKeys wakes the calculator after [MODE] turned it off.
var OnKeys = []ebiten.Key{ebiten.KeyO}

var bezel = color.RGBA{0x2A, 0x2C, 0x30, 0xFF}

// App is an ebiten.Game showing one machine.
type App struct {
	m        *calc.Machine
	finished <-chan struct{}
	view     *lcdview.View
	ghost    time.Duration

	held  map[byte]bool
	scale int
	pix   []byte        // 96x64 RGBA
	lcd   *ebiten.Image // 96x64, scaled up with nearest filtering
	grid  *ebiten.Image // pixel grid overlay at the current scale
	glass *ebiten.Image // 1x1, stretched into the panel border

	showHelp bool          // the key strip above the LCD; H toggles it
	help     *ebiten.Image // the strip, rebuilt when the width changes
	helpW    int
}

// New returns a window for m. finished must close when the game exits.
func New(m *calc.Machine, finished <-chan struct{}, ghost time.Duration) *App {
	return &App{
		m:        m,
		finished: finished,
		view:     lcdview.New(ghost),
		ghost:    ghost,
		held:     map[byte]bool{},
		// The browser page lists the keys itself, outside the canvas.
		showHelp: runtime.GOOS != "js",
	}
}

// Update implements ebiten.Game.
func (a *App) Update() error {
	select {
	case <-a.finished:
		return ebiten.Termination
	default:
	}
	for sc, keys := range KeyMap {
		down := false
		for _, k := range keys {
			if (k == ebiten.KeyEnter || k == ebiten.KeyNumpadEnter) && ebiten.IsKeyPressed(ebiten.KeyAlt) {
				continue // Alt+Enter is full screen, not [2nd]
			}
			if ebiten.IsKeyPressed(k) {
				down = true
				break
			}
		}
		if down != a.held[sc] {
			a.held[sc] = down
			if down {
				a.m.Keys.Press(sc)
			} else {
				a.m.Keys.Release(sc)
			}
		}
	}
	for _, k := range OnKeys {
		if inpututil.IsKeyJustPressed(k) {
			a.m.Keys.PressOn()
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyH) && runtime.GOOS != "js" {
		a.showHelp = !a.showHelp
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyG) {
		if a.view.Ghost > 0 {
			a.view.Ghost = 0
		} else if a.ghost > 0 {
			a.view.Ghost = a.ghost
		} else {
			a.view.Ghost = 15 * time.Millisecond
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyF11) ||
		(inpututil.IsKeyJustPressed(ebiten.KeyEnter) && ebiten.IsKeyPressed(ebiten.KeyAlt)) {
		ebiten.SetFullscreen(!ebiten.IsFullscreen())
	}
	return nil
}

// Draw implements ebiten.Game.
func (a *App) Draw(screen *ebiten.Image) {
	screen.Fill(bezel)
	sw, sh := screen.Bounds().Dx(), screen.Bounds().Dy()

	top := 0
	if a.showHelp {
		if a.help == nil || a.helpW != sw {
			if a.help != nil {
				a.help.Deallocate()
			}
			a.help = ebiten.NewImageFromImage(helpStrip(sw, helpPixel(sw, ebiten.Monitor().DeviceScaleFactor())))
			a.helpW = sw
		}
		screen.DrawImage(a.help, nil)
		top = a.help.Bounds().Dy()
	}
	sh -= top

	// Largest whole-number scale that leaves a margin, so every LCD pixel
	// is the same size.
	scale := min(sw*9/10/calc.Width, sh*9/10/calc.Height)
	scale = max(scale, 1)
	if scale != a.scale {
		a.scale = scale
		if a.grid != nil {
			a.grid.Deallocate()
		}
		a.grid = ebiten.NewImage(calc.Width*scale, calc.Height*scale)
		a.grid.WritePixels(lcdview.GridMask(scale))
	}
	if a.lcd == nil {
		a.lcd = ebiten.NewImage(calc.Width, calc.Height)
		a.pix = make([]byte, calc.Width*calc.Height*4)
		a.glass = ebiten.NewImage(1, 1)
		a.glass.Fill(lcdview.Glass())
	}

	buf, on := a.m.LCD()
	a.view.Update(buf, on, time.Now())
	a.view.RenderSmall(a.pix)
	a.lcd.WritePixels(a.pix)

	w, h := calc.Width*scale, calc.Height*scale
	x, y := (sw-w)/2, top+(sh-h)/2
	pad := max(scale*2, 4)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(float64(w+2*pad), float64(h+2*pad))
	op.GeoM.Translate(float64(x-pad), float64(y-pad))
	screen.DrawImage(a.glass, op)

	op = &ebiten.DrawImageOptions{}
	op.GeoM.Scale(float64(scale), float64(scale))
	op.GeoM.Translate(float64(x), float64(y))
	op.Filter = ebiten.FilterNearest
	screen.DrawImage(a.lcd, op)

	op = &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(x), float64(y))
	screen.DrawImage(a.grid, op)
}

// Layout implements ebiten.Game, rendering at native resolution on HiDPI
// screens so pixels stay sharp.
func (a *App) Layout(outsideWidth, outsideHeight int) (int, int) {
	s := ebiten.Monitor().DeviceScaleFactor()
	return int(float64(outsideWidth) * s), int(float64(outsideHeight) * s)
}
