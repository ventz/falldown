package calc

import "strconv"

// TI-OS display calls. Like the real ones they draw straight to the LCD,
// not to the graph buffer.

const (
	homeCols = 16
	homeRows = 8
)

// ClrLCDFull is _ClrLCDFull.
func (m *Machine) ClrLCDFull() {
	m.Spend(8_000)
	m.lcdMu.Lock()
	m.lcd = Buffer{}
	m.lcdMu.Unlock()
}

// HomeUp is _HomeUp.
func (m *Machine) HomeUp() { m.CurRow, m.CurCol = 0, 0 }

// SetCursor sets curRow/curCol (large font, 16x8 character grid).
func (m *Machine) SetCursor(row, col int) { m.CurRow, m.CurCol = row, col }

// SetPen sets penCol/penRow (small font, pixel coordinates).
func (m *Machine) SetPen(col, row int) { m.PenCol, m.PenRow = col, row }

// PutC is _PutC: one large-font character at the cursor. The cursor wraps
// to the next line after column 15.
func (m *Machine) PutC(r rune) {
	m.Spend(1_500)
	g := largeGlyph(r)
	x0, y0 := m.CurCol*6, m.CurRow*8
	m.lcdMu.Lock()
	for y := 0; y < 8; y++ {
		for x := 0; x < 6; x++ {
			on := y < 7 && x > 0 && g[y][x-1] == '#'
			m.lcd.set(x0+x, y0+y, on)
		}
	}
	m.lcdMu.Unlock()
	m.CurCol++
	if m.CurCol >= homeCols {
		m.CurCol = 0
		m.advanceRow()
	}
}

// PutS is _PutS.
func (m *Machine) PutS(s string) {
	for _, r := range s {
		m.PutC(r)
	}
}

// NewLine is _NewLine.
func (m *Machine) NewLine() {
	m.CurCol = 0
	m.advanceRow()
}

func (m *Machine) advanceRow() {
	if m.CurRow < homeRows-1 {
		m.CurRow++
	}
}

// DispHL is _DispHL: hl as a 5-character, right-justified number.
func (m *Machine) DispHL(v uint16) {
	s := strconv.Itoa(int(v))
	for len(s) < 5 {
		s = " " + s
	}
	m.PutS(s)
}

// VPutS is _VPutS: small-font text at the pen, advancing penCol. A
// character that would run off the right edge is not drawn.
func (m *Machine) VPutS(s string) {
	for _, r := range s {
		g := smallGlyph(r)
		w := len(g[0])
		if m.PenCol+w+1 > Width {
			return
		}
		m.Spend(1_200)
		m.lcdMu.Lock()
		for y := 0; y < 6; y++ {
			for x := 0; x <= w; x++ {
				on := x > 0 && y < len(g) && g[y][x-1] == '#'
				m.lcd.set(m.PenCol+x, m.PenRow+y, on)
			}
		}
		m.lcdMu.Unlock()
		m.PenCol += w + 1
	}
}

// DispOP1A is _SetXXXXOP2/_SetXXOP1 followed by _DispOP1A: a number in the
// small font at the pen, at most max characters wide.
func (m *Machine) DispOP1A(v int, max int) {
	s := strconv.Itoa(v)
	if len(s) > max {
		s = s[:max]
	}
	m.VPutS(s)
}

// DarkLine is _DarkLine. Coordinates are TI graph pixels, with (0,0) at the
// bottom-left corner of the screen.
func (m *Machine) DarkLine(x1, y1, x2, y2 int) {
	m.Spend(6_000)
	y1, y2 = Height-1-y1, Height-1-y2
	dx, dy := abs(x2-x1), -abs(y2-y1)
	sx, sy := sign(x2-x1), sign(y2-y1)
	err := dx + dy
	m.lcdMu.Lock()
	defer m.lcdMu.Unlock()
	for {
		m.lcd.set(x1, y1, true)
		if x1 == x2 && y1 == y2 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x1 += sx
		}
		if e2 <= dx {
			err += dx
			y1 += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sign(v int) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	}
	return 0
}
