package game

import (
	"context"
	"hash/fnv"
	"testing"
	"time"

	"github.com/ventz/falldown/internal/autopilot"
	"github.com/ventz/falldown/internal/calc"
	"github.com/ventz/falldown/internal/link"
)

type memStore struct{ s map[string]Settings }

func (m *memStore) Load(v string) (Settings, bool) { s, ok := m.s[v]; return s, ok }
func (m *memStore) Save(v string, s Settings)      { m.s[v] = s }

func setKey(m *calc.Machine, sc byte, down bool) {
	if down != m.Keys.Held(sc) {
		if down {
			m.Keys.Press(sc)
		} else {
			m.Keys.Release(sc)
		}
	}
}

// startOnTitle presses [2nd] the first time a menu waits and quits the
// second time (the score screen).
func startOnTitle(m *calc.Machine, onScore func()) {
	waits := 0
	m.OnWaitKey = func() {
		waits++
		switch waits {
		case 1:
			m.Keys.Press(calc.Sk2nd)
			m.Keys.Release(calc.Sk2nd)
		case 2:
			if onScore != nil {
				onScore()
			}
			m.Quit()
		}
	}
}

// Frame timing: the work between HALTs (scroll + LCD copy) runs past one
// interrupt, so speed 1 spans 2 interrupts and speed 2 spans 3.
func TestFrameTiming(t *testing.T) {
	for _, tc := range []struct {
		model calc.Model
		speed uint8
		fps   float64
	}{
		{calc.TI83Plus, 1, 60},
		{calc.TI83Plus, 2, 40},
		{calc.TI84Plus, 1, 55},
		{calc.TI84Plus, 2, 110.0 / 3},
	} {
		m := calc.NewMachine(tc.model, false, 7)
		g := New(m, V15, nil, &memStore{s: map[string]Settings{"1.5": {Speed: tc.speed, Players: 1}}})
		startOnTitle(m, nil)
		var stamps []int64
		m.OnFrame = func(n uint64) {
			x, y := g.Ball()
			l, r := autopilot.Steer(&m.GBuf, x, y)
			setKey(m, calc.SkLeft, l)
			setKey(m, calc.SkRight, r)
			if n >= 10 && n < 210 {
				stamps = append(stamps, m.Clock.Now())
			}
			if n == 210 {
				m.Quit()
			}
		}
		g.Run()
		if len(stamps) < 200 {
			t.Fatalf("%s speed %d: game ended after %d timed frames", tc.model.Name, tc.speed, len(stamps))
		}
		elapsed := float64(stamps[len(stamps)-1]-stamps[0]) / float64(tc.model.ClockHz)
		fps := float64(len(stamps)-1) / elapsed
		if fps < tc.fps*0.98 || fps > tc.fps*1.02 {
			t.Errorf("%s speed %d: %.1f fps, want %.1f", tc.model.Name, tc.speed, fps, tc.fps)
		}
	}
}

// The ball must answer the arrow keys on the very next frame and move 2
// pixels per frame while held, as the port read in the original does.
func TestInputIsPerFrame(t *testing.T) {
	m := calc.NewMachine(calc.TI83Plus, false, 3)
	g := New(m, V15, nil, nil)
	startOnTitle(m, nil)
	var xs []uint8
	m.OnFrame = func(n uint64) {
		x, _ := g.Ball()
		switch {
		case n == 1:
			// Clear the playfield so nothing blocks the ball. The ball is
			// XORed into the buffer at this point; put it back so the
			// game's second putball erases it as usual.
			m.GBuf = calc.Buffer{}
			xorSprite(&m.GBuf, g.me.x, g.me.y, ballSprite)
			setKey(m, calc.SkLeft, true)
		case n > 1 && n <= 12:
			xs = append(xs, x)
		case n == 13:
			m.Quit()
		}
	}
	g.Run()
	for i := 1; i < len(xs); i++ {
		if xs[i-1] < 2 {
			break
		}
		if xs[i] != xs[i-1]-2 {
			t.Fatalf("frame %d: x went %d -> %d, want a 2-pixel step every frame (%v)", i, xs[i-1], xs[i], xs)
		}
	}
	if xs[0] != 42 {
		t.Fatalf("first frame after pressing left: x=%d, want 42 (started at 44)", xs[0])
	}
}

// Collision tests whole bytes: a 6-pixel ball passes an 8-pixel hole only
// at x%8 == 0 or 2.
func TestHoleFitIsByteGranular(t *testing.T) {
	for x := uint8(0); x <= 90; x += 2 {
		m := calc.NewMachine(calc.TI83Plus, false, 1)
		g := &Game{m: m, v: V15, set: Settings{Speed: 1, Players: 1}}
		// A solid shelf at rows 20-23 with a single hole in byte column 5.
		for r := 20; r < 24; r++ {
			for c := range calc.BytesPerRow {
				if c != 5 {
					m.GBuf[r*calc.BytesPerRow+c] = 0xFF
				}
			}
		}
		b := ball{x: x, y: 14} // bottom row 19, just above the shelf
		g.checkcoll(&b)
		fell := b.y == 15
		want := x/8 == 5 && x%8 <= 2
		if fell != want {
			t.Errorf("x=%d: fell=%v, want %v", x, fell, want)
		}
	}
}

func TestDeathWhenPushedOffTop(t *testing.T) {
	m := calc.NewMachine(calc.TI83Plus, false, 1)
	g := &Game{m: m, v: V15}
	for r := 5; r < 9; r++ { // a shelf scrolled into the ball's bottom row
		for c := range calc.BytesPerRow {
			m.GBuf[r*calc.BytesPerRow+c] = 0xFF
		}
	}
	b := ball{x: 40, y: 0}
	if !g.checkcoll(&b) {
		t.Fatal("ball at y=0 pushed up did not die")
	}
}

func TestRowInterval(t *testing.T) {
	for _, tc := range []struct {
		score uint16
		want  int
	}{{0, 24}, {127, 24}, {128, 23}, {1280, 14}, {2944, 1}, {3072, 256}, {3200, 255}} {
		got := int(rowInterval(tc.score))
		if got == 0 {
			got = 256
		}
		if got != tc.want {
			t.Errorf("score %d: row every %d frames, want %d", tc.score, got, tc.want)
		}
	}
}

type pipeCable struct{ c *link.Conn }

func (p pipeCable) Establish(context.Context) (*link.Conn, error) { return p.c, nil }

type frameState struct {
	me, opp ball
	field   uint64 // hash of the bricks, balls removed
}

// Two linked games must agree on every brick and both balls every frame,
// and both must end on the same frame with mirrored scores.
func TestTwoPlayerLockstep(t *testing.T) {
	c1, c2 := link.NewPipe()
	var games [2]*Game
	var frames [2][]frameState
	var final [2][2]uint16
	done := make(chan int, 2)
	for i, c := range []*link.Conn{c1, c2} {
		m := calc.NewMachine(calc.TI83Plus, false, uint64(100+i))
		store := &memStore{s: map[string]Settings{"1.65": {Speed: 1, Players: 2}}}
		g := New(m, V165, pipeCable{c}, store)
		games[i] = g
		startOnTitle(m, func() { final[i] = [2]uint16{g.me.score, g.opp.score} })
		m.OnFrame = func(n uint64) {
			l, r := autopilot.Steer(&m.GBuf, g.me.x, g.me.y)
			if i == 1 && n > 600 {
				l, r = false, false // player 2 lets go so the game ends
			}
			setKey(m, calc.SkLeft, l)
			setKey(m, calc.SkRight, r)
			buf := m.GBuf
			xorSprite(&buf, g.me.x, g.me.y, ballSprite)
			xorSprite(&buf, g.opp.x, g.opp.y, oballSprite)
			h := fnv.New64a()
			h.Write(buf[:])
			frames[i] = append(frames[i], frameState{g.me, g.opp, h.Sum64()})
			if n > 20000 {
				t.Errorf("player %d: game never ended", i+1)
				m.Quit()
			}
		}
		go func() { g.Run(); done <- i }()
	}
	<-done
	<-done

	if len(frames[0]) != len(frames[1]) {
		t.Fatalf("frame counts differ: %d vs %d", len(frames[0]), len(frames[1]))
	}
	if len(frames[0]) < 600 {
		t.Fatalf("game too short to be meaningful: %d frames", len(frames[0]))
	}
	for n := range frames[0] {
		a, b := frames[0][n], frames[1][n]
		if a.field != b.field || a.me != b.opp || a.opp != b.me {
			t.Fatalf("frame %d out of sync: p1 %+v, p2 %+v", n+1, a, b)
		}
	}
	// Player 2 dies here. Both agree on its score, but player 2's own ball
	// is checked first, so player 2 leaves before scoring player 1's last
	// frame: the survivor's score can read one lower on the loser's screen.
	// That is the off-by-one the v1.65 readme lists under BUGS.
	if final[0][1] != final[1][0] {
		t.Fatalf("loser's score disagrees: p1 %v, p2 %v", final[0], final[1])
	}
	if d := int(final[0][0]) - int(final[1][1]); d < 0 || d > 1 {
		t.Fatalf("survivor's score off by %d: p1 %v, p2 %v", d, final[0], final[1])
	}
	t.Logf("%d frames in sync, scores %v", len(frames[0]), final[0])
}

func xorSprite(b *calc.Buffer, x, y uint8, rows []byte) {
	shift := uint(x) % 8
	i := int(y)*calc.BytesPerRow + int(x)/8
	for _, s := range rows {
		if i < calc.BufSize {
			b[i] ^= s >> shift
		}
		if i+1 < calc.BufSize {
			b[i+1] ^= byte(uint16(s) << (8 - shift))
		}
		i += calc.BytesPerRow
	}
}

// The virtual clock must also hold real wall-clock pace, with steady frames.
func TestRealtimePacing(t *testing.T) {
	if testing.Short() {
		t.Skip("takes about two seconds")
	}
	m := calc.NewMachine(calc.TI83Plus, true, 11)
	g := New(m, V15, nil, &memStore{s: map[string]Settings{"1.5": {Speed: 1, Players: 1}}})
	startOnTitle(m, nil)
	var stamps []time.Time
	m.OnFrame = func(n uint64) {
		x, y := g.Ball()
		l, r := autopilot.Steer(&m.GBuf, x, y)
		setKey(m, calc.SkLeft, l)
		setKey(m, calc.SkRight, r)
		if n >= 10 {
			stamps = append(stamps, time.Now())
		}
		if n == 130 {
			m.Quit()
		}
	}
	g.Run()
	if len(stamps) < 100 {
		t.Fatalf("only %d frames", len(stamps))
	}
	fps := float64(len(stamps)-1) / stamps[len(stamps)-1].Sub(stamps[0]).Seconds()
	worst := time.Duration(0)
	for i := 1; i < len(stamps); i++ {
		worst = max(worst, stamps[i].Sub(stamps[i-1]))
	}
	t.Logf("%.2f fps wall clock, worst frame %v", fps, worst)
	if fps < 58.5 || fps > 61.5 {
		t.Errorf("%.2f fps, want 60", fps)
	}
	if worst > 25*time.Millisecond {
		t.Errorf("worst frame %v, want under 25ms (16.7ms nominal)", worst)
	}
}
