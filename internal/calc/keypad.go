package calc

import "sync"

// TI-83 Plus _GetCSC scan codes used by Fall Down.
const (
	SkDown  byte = 1
	SkLeft  byte = 2
	SkRight byte = 3
	SkUp    byte = 4
	SkEnter byte = 9
	SkClear byte = 15
	SkAlpha byte = 48
	Sk2nd   byte = 54
	SkMode  byte = 55
)

// Keypad holds key state shared between the UI goroutine, which feeds it,
// and the game goroutine, which reads it.
type Keypad struct {
	mu    sync.Mutex
	held  [64]bool
	queue []byte
	on    bool // [ON] pressed since the last check
}

// NewKeypad returns an idle keypad.
func NewKeypad() *Keypad { return &Keypad{} }

// Press records a key going down. It is held until Release, and queued
// once for GetCSC.
func (k *Keypad) Press(sc byte) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if int(sc) < len(k.held) {
		k.held[sc] = true
	}
	// The OS keeps a single pending scan code; keep a short queue so a
	// quick double tap at a menu isn't lost, but never build a backlog.
	if len(k.queue) >= 2 {
		k.queue = k.queue[1:]
	}
	k.queue = append(k.queue, sc)
}

// Release records a key coming up.
func (k *Keypad) Release(sc byte) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if int(sc) < len(k.held) {
		k.held[sc] = false
	}
}

// PressOn records the [ON] key, which has no scan code.
func (k *Keypad) PressOn() {
	k.mu.Lock()
	k.on = true
	k.mu.Unlock()
}

// Held reports whether a key is down right now.
func (k *Keypad) Held(sc byte) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return int(sc) < len(k.held) && k.held[sc]
}

func (k *Keypad) pop() byte {
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.queue) == 0 {
		return 0
	}
	sc := k.queue[0]
	k.queue = k.queue[1:]
	return sc
}

func (k *Keypad) drain() {
	k.mu.Lock()
	k.queue = k.queue[:0]
	k.mu.Unlock()
}

func (k *Keypad) clearOn() {
	k.mu.Lock()
	k.on = false
	k.mu.Unlock()
}

func (k *Keypad) takeOn() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	on := k.on
	k.on = false
	return on
}
