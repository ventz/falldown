// Package game is a port of Fall Down for the TI-83 Plus, following the Z80
// source routine by routine (scripts/fetch-originals.sh downloads it).
//
// Comments quote the original labels so the two can be read side by side.
// Values that were bytes in the original are uint8 here, so they wrap the
// same way: the ball dying when bally goes "negative", the brick interval
// wrapping past score 3072, and so on.
package game

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ventz/falldown/internal/calc"
	"github.com/ventz/falldown/internal/link"
)

// leave is the byte v1.65 sends when a player quits mid-game.
const leave = 100

var (
	brick = []byte{ // block:
		0b11111111,
		0b10000001,
		0b10111111,
		0b11111111,
	}
	ballSprite = []byte{ // ball:
		0b01111000,
		0b10110100,
		0b11001100,
		0b11001100,
		0b10110100,
		0b01111000,
	}
	oballSprite = []byte{ // oball: the opponent, drawn inside-out
		0b01111000,
		0b11001100,
		0b10110100,
		0b10110100,
		0b11001100,
		0b01111000,
	}
)

// Cable establishes a link to another calculator.
type Cable interface {
	Establish(ctx context.Context) (*link.Conn, error)
}

type ball struct {
	x, y  uint8
	score uint16
}

// Game is one running copy of Fall Down.
type Game struct {
	m     *calc.Machine
	v     Variant
	cable Cable
	store Store
	set   Settings

	me, opp  ball  // ballx/bally/score, oballx/obally/oscore
	state    uint8 // 1 = send, 0 = recv (2-player)
	temp     uint8 // 2 = single player, else the state at brick time
	sendrecv uint8
	conn     *link.Conn
}

// New prepares a game. cable may be nil when no link is configured; store
// may be nil to keep high scores in memory only.
func New(m *calc.Machine, v Variant, cable Cable, store Store) *Game {
	g := &Game{m: m, v: v, cable: cable, store: store}
	g.set = Settings{Speed: v.DefaultSpeed, Players: 1}
	if store != nil {
		if s, ok := store.Load(v.Version); ok {
			g.set = s
		}
	}
	if g.set.Speed != 1 && g.set.Speed != 2 {
		g.set.Speed = v.DefaultSpeed
	}
	if g.set.Players != 2 || !v.Multiplayer {
		g.set.Players = 1
	}
	return g
}

// Run plays until the player quits from the title screen.
func (g *Game) Run() {
	g.m.Run(func() {
		for g.title() {
			g.play()
		}
	})
	if g.conn != nil {
		g.conn.Close()
	}
}

// Ball returns the player's ball position, for tests and the autopilot.
func (g *Game) Ball() (x, y uint8) { return g.me.x, g.me.y }

// Score returns the player's current score.
func (g *Game) Score() uint16 { return g.me.score }

func (g *Game) twoPlayer() bool { return g.set.Players == 2 }

func (g *Game) save() {
	if g.store != nil {
		g.store.Save(g.v.Version, g.set)
	}
}

// title is the "start:" screen. It returns false when the player quits.
func (g *Game) title() bool {
	m := g.m
	for {
		m.ClrLCDFull()
		m.SetCursor(0, 4)
		m.PutS("Falldown")
		m.DarkLine(0, 55, 94, 55)
		m.SetPen(1, 10)
		m.VPutS(g.v.Author)
		m.SetPen(4, 22)
		m.VPutS("2nd - Start")
		speedRow := 29
		if g.v.Multiplayer {
			m.SetPen(4, 29)
			m.VPutS("Right - Players: ")
			speedRow = 36
		}
		m.SetPen(4, speedRow)
		m.VPutS("ALPHA - Speed:")
		m.SetPen(4, speedRow+7)
		m.VPutS("CLEAR - Exit")
		m.SetPen(1, 55)
		m.VPutS("High: ")
		m.DispOP1A(int(g.set.High), 5)

		updatm := func() {
			m.SetPen(53, speedRow)
			m.DispOP1A(int(g.set.Speed), 1)
			if g.v.Multiplayer {
				m.SetPen(56, 29)
				m.DispOP1A(int(g.set.Players), 1)
			}
		}
		updatm()

	menul:
		for {
			switch m.WaitCSC() {
			case calc.SkClear:
				return false
			case calc.SkMode:
				if g.v.ExtraExitKeys {
					return false
				}
			case calc.SkAlpha:
				g.set.Speed ^= 0b11 // toggle between 1 and 2
				g.save()
				updatm()
			case calc.SkRight:
				if g.v.Multiplayer {
					g.set.Players ^= 0b11
					g.save()
					updatm()
				}
			case calc.Sk2nd:
				if !g.twoPlayer() {
					return true
				}
				if g.waitForPartner() {
					return true
				}
				break menul // cancelled: redraw the title
			}
		}
	}
}

// waitForPartner shows "Waiting..." until the other calculator links up.
func (g *Game) waitForPartner() bool {
	m := g.m
	m.GrBufClr()
	m.FastCopy()
	m.SetCursor(0, 0)
	m.PutS("Waiting...")
	if g.cable == nil {
		g.linkError("2-player mode needs a link: start with -host or -join")
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		c   *link.Conn
		err error
	}
	done := make(chan result, 1)
	go func() {
		c, err := g.cable.Establish(ctx)
		done <- result{c, err}
	}()
	for {
		if k := m.GetCSC(); k == calc.SkClear || k == calc.SkMode {
			cancel()
			if r := <-done; r.c != nil {
				r.c.Close()
			}
			return false
		}
		select {
		case r := <-done:
			if r.err != nil {
				g.linkError(fmt.Sprintf("link: %v", r.err))
				return false
			}
			g.conn = r.c
			if r.c.P1() {
				g.state = 1 // p1: go into send state
			} else {
				g.state = 0 // p2: go into recv state
			}
			m.Clock.Resync()
			return true
		default:
		}
		m.Halt()
	}
}

// linkError is the port's own addition: the calculator had no failure a
// TCP connection can have, so say so on the LCD as well as the console
// (which a Windows build doesn't show), and wait for a key.
func (g *Game) linkError(msg string) {
	log.Print(msg)
	m := g.m
	m.NewLine()
	m.PutS("Link error")
	m.FastCopy()
	m.WaitCSC()
}

// rowInterval is how many frames pass between new rows: 24 - score/128,
// computed in 8 bits as the original did, so 0 means 256.
func rowInterval(score uint16) uint8 { return 24 - uint8((score*2)>>8) }

// play is "begin:" through the end of the score screen.
func (g *Game) play() {
	m := g.m
	m.GrBufClr()
	g.sendrecv = 1
	g.me = ball{x: 44}
	g.opp = ball{x: 44}

	for { // init:
		if !g.randomblocks() {
			g.exit()
			return
		}
		// The rows come closer together as the score climbs.
		hl := g.me.score
		if g.temp == 0 { // player 2 times rows off player 1's score
			hl = g.opp.score
		}
		blockcnt := rowInterval(hl)

		for { // sloop:
			g.scroll()
			if g.checkcoll(&g.me) {
				g.scoreScreen()
				return
			}
			if g.twoPlayer() && g.checkcoll(&g.opp) {
				g.scoreScreen()
				return
			}
			g.putball()
			m.FastCopy()
			g.putball()
			for range g.set.Speed { // delay: halt / djnz delay
				m.Halt()
			}
			if !g.move() {
				g.exit()
				return
			}
			m.Spend(calc.CostMisc)
			blockcnt--
			if blockcnt == 0 {
				break
			}
		}
	}
}

// move reads input (and in 2-player mode trades positions over the link).
// It returns false when the game should end via "exit:".
func (g *Game) move() bool {
	m := g.m
	if !g.twoPlayer() {
		g.ckkey()
		switch m.GetCSC() {
		case calc.SkClear:
			return false
		case calc.SkMode:
			m.PowerOff() // "pause": turn the calculator off until [ON]
		}
		return true
	}
	for {
		if g.state == 0 { // getmov:
			g.state = 1
			b, ok := g.getbytesafe()
			if !ok {
				return false
			}
			g.opp.x = b
		} else {
			g.state = 0
			g.ckkey()
			g.sendbyte(g.me.x)
		}
		g.sendrecv ^= 0b11 // swapturn:
		if g.sendrecv != 2 {
			return true
		}
	}
}

// ckkey reads the arrow keys straight from the keypad port, so the ball
// answers to whatever is held at this instant, every frame.
func (g *Game) ckkey() {
	port := g.m.ArrowPort()
	switch {
	case port&0b0010 == 0: // [(left)]
		if g.me.x < 2 {
			return // offscreen
		}
		g.xcheck(g.me.x-2, g.me.x-2)
	case port&0b0100 == 0: // [(right)]
		tmpx := g.me.x + 2
		if tmpx >= 96-4 {
			return // offscreen
		}
		g.xcheck(tmpx, tmpx+5)
	}
}

// xcheck moves the ball to tmpx unless the byte at column px is occupied
// on the ball's top or bottom row. Only those two rows are tested.
func (g *Game) xcheck(tmpx, px uint8) {
	if g.m.GetPixByte(px, g.me.y) != 0 {
		return // something in way
	}
	if g.m.GetPixByte(px, g.me.y+5) != 0 {
		return
	}
	g.me.x = tmpx
}

// randomblocks draws a new row of bricks at the bottom of the buffer.
// It returns false if the link dropped while receiving the layout.
func (g *Game) randomblocks() bool {
	m := g.m
	const row = 60 * calc.BytesPerRow
	g.temp = 2
	if g.twoPlayer() {
		g.temp = g.state
	}
	pick := func(n int) (byte, bool) {
		switch g.temp {
		case 2:
			return m.Random(n), true
		case 0:
			return g.getbytesafe() // recv the random number
		default:
			a := m.Random(n)
			g.sendbyte(a) // send it to player 2
			return a, true
		}
	}
	for i := range calc.BytesPerRow { // 12 blocks
		a, ok := pick(4) // one chance in 4 of a hole
		if !ok {
			return false
		}
		if a == 0 {
			continue // skip space
		}
		m.Spend(calc.CostBlockDraw)
		for r, b := range brick {
			m.GBuf[row+i+r*calc.BytesPerRow] = b
		}
	}

	// The original meant to count the bricks and only force a hole when the
	// row was solid, but its loop tested the first brick twelve times. So a
	// hole is carved whenever column 0 is a brick. v1.65 kept the behavior
	// with a shorter test.
	if g.v.Multiplayer {
		if m.GBuf[row] == 0 {
			return true
		}
	} else if m.GBuf[row]&1 == 0 {
		return true
	}
	a, ok := pick(12) // put random space
	if !ok {
		return false
	}
	for r := range 4 {
		if i := row + int(a) + r*calc.BytesPerRow; i < calc.BufSize {
			m.GBuf[i] = 0
		}
	}
	return true
}

// scroll moves the whole buffer up one pixel and clears the bottom row.
func (g *Game) scroll() {
	b := &g.m.GBuf
	g.m.Spend(calc.CostScroll)
	copy(b[:calc.BufSize-calc.BytesPerRow], b[calc.BytesPerRow:])
	clear(b[calc.BufSize-calc.BytesPerRow:])
}

// checkcoll lets a ball fall, holds it on a shelf, or pushes it up with
// the scroll. It reports true when the ball has been pushed off the top.
func (g *Game) checkcoll(b *ball) bool {
	m := g.m
	if m.GetPixByte(b.x, b.y+6) == 0 && m.GetPixByte(b.x+5, b.y+6) == 0 {
		b.score++ // inc score
		b.y++     // move the ball down
		if b.y >= 60-5 {
			b.y--
		}
		return false
	}
	// ndown:
	if m.GetPixByte(b.x, b.y+5) == 0 && m.GetPixByte(b.x+5, b.y+5) == 0 {
		return false
	}
	b.y-- // moveup:
	return b.y&0x80 != 0
}

func (g *Game) putball() {
	g.m.Sprite(g.me.x, g.me.y, ballSprite)
	if g.twoPlayer() {
		g.m.Sprite(g.opp.x, g.opp.y, oballSprite)
	}
}

// exit is the quit path: tell the other player, then show the scores.
func (g *Game) exit() {
	if g.twoPlayer() && g.conn != nil {
		g.sendbyte(leave)
	}
	g.scoreScreen()
}

// scoreScreen is "nolsnd:" through "exitl:".
func (g *Game) scoreScreen() {
	m := g.m
	if g.conn != nil {
		g.conn.Close()
		g.conn = nil
	}
	m.ClrLCDFull()
	m.HomeUp()
	m.PutS(g.v.ScoreLabel)
	m.DispHL(g.me.score)
	if g.twoPlayer() {
		m.NewLine()
		m.PutS("P2's Score:")
		m.DispHL(g.opp.score)
		m.NewLine()
		switch {
		case g.me.score < g.opp.score:
			m.PutS("You Lose!")
		case g.me.score == g.opp.score:
			m.PutS("It's a Tie!")
		default:
			m.PutS("You Win!")
		}
	}
	// cphlde / jr c: a tie with the old high counts as a new high, so even
	// a first game of 0 says "New High!".
	if g.me.score >= g.set.High {
		g.set.High = g.me.score
		g.save()
		m.NewLine()
		m.PutS("New High!")
	}
	for {
		switch m.WaitCSC() {
		case calc.Sk2nd:
			return
		case calc.SkClear, calc.SkMode:
			if g.v.ExtraExitKeys {
				return
			}
		}
	}
}

func (g *Game) sendbyte(b byte) {
	g.m.Spend(calc.CostSendByte)
	if g.conn != nil {
		g.conn.Send(b)
	}
}

// getbytesafe waits for a byte from the other player. [CLEAR] or [MODE]
// abandons the game, as does the other side leaving.
func (g *Game) getbytesafe() (byte, bool) {
	m := g.m
	if g.conn == nil {
		return 0, false
	}
	recv := g.conn.Recv()
	got := func(b byte, ok bool) (byte, bool) {
		if !ok || b == leave {
			return 0, false
		}
		m.Spend(calc.CostMisc)
		return b, true
	}
	for { // linkwait:
		if k := m.GetCSC(); k == calc.SkClear || k == calc.SkMode {
			return 0, false
		}
		select {
		case b, ok := <-recv:
			return got(b, ok)
		default:
		}
		select {
		case b, ok := <-recv:
			return got(b, ok)
		case <-time.After(m.Clock.UntilNextInterrupt()):
			m.Halt()
		case <-m.Done():
			m.Halt() // unwinds
		}
	}
}
