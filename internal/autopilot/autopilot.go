// Package autopilot steers the ball well enough to keep a game going for
// tests and screenshots. It is not part of the game.
package autopilot

import "github.com/ventz/falldown/internal/calc"

// Steer looks at the first shelf below the ball at (x, y) and returns which
// arrow to hold to line up with the nearest hole. Collision works on whole
// bytes, so the 6-pixel ball fits through an 8-pixel hole only when it sits
// at x%8 == 0 or 2.
func Steer(buf *calc.Buffer, x, y uint8) (left, right bool) {
	shelf := -1
	for r := int(y) + 6; r < calc.Height && shelf < 0; r++ {
		for c := range calc.BytesPerRow {
			if buf[r*calc.BytesPerRow+c] != 0 {
				shelf = r
				break
			}
		}
	}
	if shelf < 0 {
		return false, false
	}
	best, bestDist := -1, 1<<30
	for c := range calc.BytesPerRow {
		if buf[shelf*calc.BytesPerRow+c] != 0 {
			continue
		}
		for _, t := range []int{c * 8, c*8 + 2} {
			if t > 90 {
				continue
			}
			d := t - int(x)
			if d < 0 {
				d = -d
			}
			if d < bestDist {
				best, bestDist = t, d
			}
		}
	}
	switch {
	case best < 0:
		return false, false
	case best < int(x):
		return true, false
	case best > int(x):
		return false, true
	}
	return false, false
}
