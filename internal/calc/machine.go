// Package calc emulates the slice of a TI-83 Plus that Fall Down touches:
// the 96x64 graph buffer and LCD, the keypad, the Ion/MirageOS library
// routines, a handful of TI-OS text calls, and HALT-based timing.
//
// The game code in package game is a line-by-line port of the Z80 source,
// so these routines keep the original semantics, quirks included.
package calc

import (
	"math/rand/v2"
	"sync"
	"time"
)

const (
	Width       = 96
	Height      = 64
	BytesPerRow = Width / 8
	BufSize     = BytesPerRow * Height // 768, the size of plotSScreen
)

// Buffer is a packed 1bpp screen, 12 bytes per row, MSB = leftmost pixel.
// This is the layout of both the graph buffer and the LCD driver's RAM.
type Buffer [BufSize]byte

// Pixel reports whether the pixel at (x, y) is dark.
func (b *Buffer) Pixel(x, y int) bool {
	if x < 0 || x >= Width || y < 0 || y >= Height {
		return false
	}
	return b[y*BytesPerRow+x/8]&(0x80>>(x%8)) != 0
}

func (b *Buffer) set(x, y int, on bool) {
	if x < 0 || x >= Width || y < 0 || y >= Height {
		return
	}
	i, mask := y*BytesPerRow+x/8, byte(0x80>>(x%8))
	if on {
		b[i] |= mask
	} else {
		b[i] &^= mask
	}
}

// quitSignal unwinds the game goroutine when the window closes.
type quitSignal struct{}

// Machine is one emulated calculator.
type Machine struct {
	GBuf  Buffer // plotSScreen, where the game draws
	Clock *Clock
	Keys  *Keypad

	// Home-screen cursor (large font) and pen (small font).
	CurRow, CurCol int
	PenCol, PenRow int

	lcdMu sync.Mutex
	lcd   Buffer
	lcdOn bool

	// Optional hooks for tests and the screenshot tool. Both run on the
	// game goroutine.
	OnFrame   func(frame uint64) // after each FastCopy
	OnWaitKey func()             // each time a menu starts waiting for a key

	frames uint64
	rng    *rand.Rand
	quit   chan struct{}
	once   sync.Once
}

// NewMachine builds a calculator. seed fixes the random brick layout for
// tests; pass 0 for a random seed.
func NewMachine(model Model, realtime bool, seed uint64) *Machine {
	if seed == 0 {
		seed = uint64(time.Now().UnixNano())
	}
	return &Machine{
		Clock: NewClock(model, realtime),
		Keys:  NewKeypad(),
		lcdOn: true,
		rng:   rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15)),
		quit:  make(chan struct{}),
	}
}

// Run executes program on the calling goroutine and returns when it exits
// or when Quit is called.
func (m *Machine) Run(program func()) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(quitSignal); !ok {
				panic(r)
			}
		}
	}()
	program()
}

// Quit stops a running program at its next wait point.
func (m *Machine) Quit() { m.once.Do(func() { close(m.quit) }) }

// Done is closed once Quit has been called.
func (m *Machine) Done() <-chan struct{} { return m.quit }

func (m *Machine) checkQuit() {
	select {
	case <-m.quit:
		panic(quitSignal{})
	default:
	}
}

// LCD returns a copy of what the display is showing and whether it is on.
func (m *Machine) LCD() (Buffer, bool) {
	m.lcdMu.Lock()
	defer m.lcdMu.Unlock()
	return m.lcd, m.lcdOn
}

// ---- timing ----

// Spend charges n T-states of work to the virtual clock.
func (m *Machine) Spend(n int64) { m.Clock.Spend(n) }

// Halt is the Z80 HALT instruction with interrupts enabled.
func (m *Machine) Halt() {
	m.checkQuit()
	m.Clock.Halt()
	m.checkQuit()
}

// ---- Ion / MirageOS library ----

// FastCopy is iFastCopy: copy the graph buffer to the LCD.
func (m *Machine) FastCopy() {
	m.Spend(CostFastCopy)
	m.lcdMu.Lock()
	m.lcd = m.GBuf
	m.lcdMu.Unlock()
	m.frames++
	if m.OnFrame != nil {
		m.OnFrame(m.frames)
	}
}

// GetPixByte is iGetPix followed by "ld a,(hl)": the whole graph-buffer byte
// containing pixel (x, y). Fall Down tests this byte for zero, so collision
// is 8 pixels wide, not one pixel.
func (m *Machine) GetPixByte(x, y uint8) byte {
	m.Spend(CostGetPix)
	i := int(y)*BytesPerRow + int(x)/8
	if i >= BufSize {
		return 0
	}
	return m.GBuf[i]
}

// Sprite is iSprite (ionPutSprite): XOR an 8-pixel-wide sprite into the
// graph buffer at (x, y). Like the original it does not clip; the second
// byte of a row may spill into column 0 of the next row.
func (m *Machine) Sprite(x, y uint8, rows []byte) {
	m.Spend(CostSprite)
	shift := uint(x) % 8
	i := int(y)*BytesPerRow + int(x)/8
	for _, s := range rows {
		if i < BufSize {
			m.GBuf[i] ^= s >> shift
		}
		if i+1 < BufSize {
			m.GBuf[i+1] ^= byte(uint16(s) << (8 - shift))
		}
		i += BytesPerRow
	}
}

// Random is iRandom: a value in 0..n-1.
func (m *Machine) Random(n int) byte {
	m.Spend(CostRandom)
	return byte(m.rng.IntN(n))
}

// GrBufClr is _GrBufClr.
func (m *Machine) GrBufClr() {
	m.Spend(4_000)
	m.GBuf = Buffer{}
}

// ---- keypad ----

// GetCSC is _GetCSC: the scan code of the last newly pressed key, or 0.
func (m *Machine) GetCSC() byte {
	m.checkQuit()
	m.Spend(CostGetCSC)
	return m.Keys.pop()
}

// WaitCSC polls GetCSC until a key arrives. The OS only scans the keypad
// in its interrupt handler, so halting between polls is equivalent to the
// original busy loop.
func (m *Machine) WaitCSC() byte {
	if m.OnWaitKey != nil {
		m.OnWaitKey()
	}
	for {
		if k := m.GetCSC(); k != 0 {
			m.Clock.Resync()
			return k
		}
		m.Halt()
	}
}

// ArrowPort reads keypad group $FE directly (out (1),$FE / in a,(1)).
// Bits are active low: 0 down, 1 left, 2 right, 3 up.
func (m *Machine) ArrowPort() byte {
	m.Spend(CostKeyPort)
	v := byte(0xFF)
	for bit, sc := range []byte{SkDown, SkLeft, SkRight, SkUp} {
		if m.Keys.Held(sc) {
			v &^= 1 << bit
		}
	}
	return v
}

// PowerOff is what Fall Down does on [MODE]: "ld a,1 / out (3),a / halt".
// Only the ON-key interrupt stays enabled and the calculator drops into
// low-power mode, blanking the LCD until [ON] is pressed.
func (m *Machine) PowerOff() {
	m.lcdMu.Lock()
	m.lcdOn = false
	m.lcdMu.Unlock()
	m.Keys.clearOn()
	for !m.Keys.takeOn() {
		select {
		case <-m.quit:
			panic(quitSignal{})
		case <-time.After(10 * time.Millisecond):
		}
	}
	m.Keys.drain()
	m.lcdMu.Lock()
	m.lcdOn = true
	m.lcdMu.Unlock()
	m.Clock.Resync()
}
