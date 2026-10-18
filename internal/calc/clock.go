package calc

import "time"

// Model describes the timing of one calculator family.
//
// Fall Down paces itself with HALT, which sleeps until the next hardware
// timer interrupt. A frame therefore lasts a whole number of interrupt
// periods: the work done between HALTs (scroll, collision, LCD copy) runs
// past one interrupt, so the first HALT waits for the second one.
type Model struct {
	Name    string
	ClockHz int64 // Z80 clock (asm programs run at 6 MHz on every model)
	IntHz   int64 // OS timer interrupt rate
	ISRCost int64 // T-states spent in the OS interrupt handler per interrupt
}

var (
	// TI83Plus is the original TI-83 Plus: ~120 interrupts per second.
	TI83Plus = Model{Name: "TI-83 Plus", ClockHz: 6_000_000, IntHz: 120, ISRCost: 1500}
	// TI84Plus covers the TI-83 Plus SE / TI-84 Plus family: ~110 interrupts
	// per second, Fall Down still runs at 6 MHz.
	TI84Plus = Model{Name: "TI-84 Plus", ClockHz: 6_000_000, IntHz: 110, ISRCost: 1500}
)

// Estimated T-state costs of the routines Fall Down calls. They only need
// to be right to within a few thousand cycles: what matters is whether a
// frame's work crosses one interrupt boundary or two.
const (
	CostScroll    = 16_200 // ldir of 756 bytes + clearing the bottom row
	CostFastCopy  = 56_000 // MirageOS iFastCopy, LCD-delay bound
	CostSprite    = 1_000  // iSprite, 6 rows, XOR
	CostGetPix    = 150    // iGetPix + the byte test
	CostGetCSC    = 600    // bcall overhead + reading the scan code
	CostKeyPort   = 120    // out (1) / in (1)
	CostRandom    = 250    // iRandom
	CostBlockDraw = 300    // drawing one 8x4 brick
	CostSendByte  = 6_000  // TIOS _SendAByte to a waiting link assist
	CostMisc      = 400
)

// Clock is a virtual Z80 clock. Game code spends T-states as it runs and
// calls Halt to wait for the next interrupt; Halt sleeps in real time so
// that virtual and wall time stay in step.
type Clock struct {
	model    Model
	period   int64 // T-states between interrupts
	t        int64 // current virtual time in T-states
	next     int64 // virtual time of the next interrupt
	realtime bool

	anchorReal time.Time
	anchorT    int64
}

// NewClock returns a clock for model. When realtime is false Halt never
// sleeps, which is what tests and screenshot rendering want.
func NewClock(model Model, realtime bool) *Clock {
	c := &Clock{model: model, realtime: realtime}
	c.period = model.ClockHz / model.IntHz
	c.next = c.period
	c.anchorReal = time.Now()
	return c
}

// Model returns the timing model in use.
func (c *Clock) Model() Model { return c.model }

// Now returns the virtual time in T-states.
func (c *Clock) Now() int64 { return c.t }

// Spend advances virtual time by n T-states, servicing any interrupts that
// fall inside that window (each costs ISRCost on top).
func (c *Clock) Spend(n int64) {
	c.t += n
	for c.t >= c.next {
		c.t += c.model.ISRCost
		c.next += c.period
	}
}

// Halt sleeps until the next interrupt, runs the handler, and returns.
func (c *Clock) Halt() {
	if c.t < c.next {
		c.t = c.next
	}
	c.t += c.model.ISRCost
	c.next += c.period
	c.sync()
}

// UntilNextInterrupt reports the wall-clock time left before the next
// interrupt is due.
func (c *Clock) UntilNextInterrupt() time.Duration {
	if !c.realtime {
		return 0
	}
	target := c.wallAt(c.next)
	if d := time.Until(target); d > 0 {
		return d
	}
	return 0
}

// Resync re-anchors virtual time to the wall clock after a wait of unknown
// length (a link transfer, a key press at a menu).
func (c *Clock) Resync() {
	c.anchorReal = time.Now()
	c.anchorT = c.t
}

func (c *Clock) wallAt(t int64) time.Time {
	ns := (t - c.anchorT) * int64(time.Second) / c.model.ClockHz
	return c.anchorReal.Add(time.Duration(ns))
}

func (c *Clock) sync() {
	if !c.realtime {
		return
	}
	target := c.wallAt(c.t)
	now := time.Now()
	if d := target.Sub(now); d > 0 {
		preciseSleep(d)
		return
	}
	// Running late by more than a couple of frames means we were blocked
	// (window drag, slow link). Don't try to catch up in a burst.
	if now.Sub(target) > 50*time.Millisecond {
		c.Resync()
	}
}
