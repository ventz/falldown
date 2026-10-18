package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"

	"github.com/ventz/falldown/internal/autopilot"
	"github.com/ventz/falldown/internal/calc"
	"github.com/ventz/falldown/internal/game"
	"github.com/ventz/falldown/internal/lcdview"
)

func setKey(m *calc.Machine, sc byte, down bool) {
	if down != m.Keys.Held(sc) {
		if down {
			m.Keys.Press(sc)
		} else {
			m.Keys.Release(sc)
		}
	}
}

// screenshots plays one unattended game on a non-realtime clock and saves
// the title screen, frame 400 and the score screen.
func screenshots(dir string, v game.Variant, model calc.Model, seed uint64) error {
	if seed == 0 {
		seed = 1983
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	m := calc.NewMachine(model, false, seed)
	g := game.New(m, v, nil, nil)

	var err error
	save := func(name string) {
		buf, on := m.LCD()
		path := filepath.Join(dir, fmt.Sprintf("falldown-%s-%s.png", v.Version, name))
		f, e := os.Create(path)
		if e != nil {
			err = e
			return
		}
		defer f.Close()
		if e := png.Encode(f, lcdview.Image(buf, on, 6)); e != nil {
			err = e
			return
		}
		fmt.Println("wrote", path)
	}

	waits := 0
	m.OnWaitKey = func() {
		waits++
		switch waits {
		case 1:
			save("title")
			m.Keys.Press(calc.Sk2nd)
			m.Keys.Release(calc.Sk2nd)
		case 2:
			save("score")
			m.Quit()
		}
	}
	m.OnFrame = func(n uint64) {
		// Let the autopilot play for a while, then let go so the game ends.
		left, right := false, false
		if n < 600 {
			x, y := g.Ball()
			left, right = autopilot.Steer(&m.GBuf, x, y)
		}
		setKey(m, calc.SkLeft, left)
		setKey(m, calc.SkRight, right)
		if n == 400 {
			save("game")
		}
	}
	g.Run()
	return err
}
