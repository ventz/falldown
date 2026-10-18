// ===== Fall Down core: a JS port of github.com/ventz/falldown (Go, MIT) =====
// internal/calc (machine.go, clock.go, keypad.go, text.go) and
// internal/game (falldown.go, variant.go, settings.go). Function names and
// the original asm labels are kept in comments so this can be read against
// the Go. Blocking Go code becomes generators: every `yield` is a HALT.

// ---- calc/machine.go ----
const W = 96, H = 64, BPR = W / 8, BUFSIZE = BPR * H; // plotSScreen: 768 bytes

// keypad.go: TI-83 Plus _GetCSC scan codes used by Fall Down.
const SkDown = 1, SkLeft = 2, SkRight = 3, SkUp = 4, SkEnter = 9,
  SkClear = 15, SkAlpha = 48, Sk2nd = 54, SkMode = 55;

// ---- calc/clock.go ----
const MODELS = {
  '83p': { name: 'TI-83 Plus', clockHz: 6000000, intHz: 120, isrCost: 1500 },
  '84p': { name: 'TI-84 Plus', clockHz: 6000000, intHz: 110, isrCost: 1500 },
};
// Estimated T-state costs (clock.go).
const CostScroll = 16200, CostFastCopy = 56000, CostSprite = 1000,
  CostGetPix = 150, CostGetCSC = 600, CostKeyPort = 120, CostRandom = 250,
  CostBlockDraw = 300, CostSendByte = 6000, CostMisc = 400;

const nowMs = () => (typeof performance !== 'undefined' ? performance.now() : Date.now());

// Clock is a virtual Z80 clock. Halt advances to the next interrupt; the
// driver (not the clock) sleeps in real time until wallAt(t).
class Clock {
  constructor(model, realtime) {
    this.model = model; this.realtime = realtime;
    this.period = Math.floor(model.clockHz / model.intHz); // T-states between interrupts
    this.t = 0; this.next = this.period;
    this.anchorReal = nowMs(); this.anchorT = 0;
  }
  now() { return this.t; }
  // Spend: advance n T-states, servicing interrupts inside the window.
  spend(n) {
    this.t += n;
    while (this.t >= this.next) { this.t += this.model.isrCost; this.next += this.period; }
  }
  // Halt: sleep until the next interrupt, run the handler.
  halt() {
    if (this.t < this.next) this.t = this.next;
    this.t += this.model.isrCost;
    this.next += this.period;
  }
  resync() { this.anchorReal = nowMs(); this.anchorT = this.t; }
  wallAt(t) { return this.anchorReal + (t - this.anchorT) * 1000 / this.model.clockHz; }
}

// ---- calc/keypad.go ----
class Keypad {
  constructor() { this.held = new Array(64).fill(false); this.queue = []; this.on = false; }
  press(sc) {
    if (sc < 64) this.held[sc] = true;
    // The OS keeps one pending scan code; the Go port keeps up to two.
    if (this.queue.length >= 2) this.queue.shift();
    this.queue.push(sc);
  }
  release(sc) { if (sc < 64) this.held[sc] = false; }
  pressOn() { this.on = true; }
  isHeld(sc) { return sc < 64 && this.held[sc]; }
  pop() { return this.queue.length ? this.queue.shift() : 0; }
  drain() { this.queue.length = 0; }
  clearOn() { this.on = false; }
  takeOn() { const o = this.on; this.on = false; return o; }
}

// math/rand/v2 PCG (DXSM) + Rand.IntN, bit-exact with Go so a seed gives the
// same bricks as the Go port (NewMachine seeds PCG(seed, seed^0x9E37...)).
const M64 = (1n << 64n) - 1n, M128 = (1n << 128n) - 1n;
const PCG_MUL = (2549297995355413924n << 64n) | 4865540595714422341n;
const PCG_INC = (6364136223846793005n << 64n) | 1442695040888963407n;
class PCG {
  constructor(seed1, seed2) { this.s = ((BigInt.asUintN(64, seed1) << 64n) | BigInt.asUintN(64, seed2)); }
  uint64() {
    this.s = (this.s * PCG_MUL + PCG_INC) & M128; // state = state*mul + inc
    let hi = this.s >> 64n; const lo = this.s & M64;
    hi ^= hi >> 32n; hi = (hi * 0xda942042e4dd58b5n) & M64;
    hi ^= hi >> 48n; hi = (hi * (lo | 1n)) & M64;
    return hi;
  }
  intN(n) { // Rand.uint64n
    const N = BigInt(n);
    if ((n & (n - 1)) === 0) return Number(this.uint64() & (N - 1n));
    let p = this.uint64() * N, hi = p >> 64n, lo = p & M64;
    if (lo < N) {
      const thresh = ((1n << 64n) - N) % N;
      while (lo < thresh) { p = this.uint64() * N; hi = p >> 64n; lo = p & M64; }
    }
    return Number(hi);
  }
}

class QuitSignal {}
const SLEEP = 'sleep'; // yielded by PowerOff: wait ~10 ms of wall time

// Machine is one emulated calculator.
class Machine {
  constructor(model, realtime, seed) {
    if (!seed) seed = BigInt(Date.now()) * 1000000n + BigInt(Math.floor(Math.random() * 1e6));
    seed = BigInt.asUintN(64, BigInt(seed));
    this.gbuf = new Uint8Array(BUFSIZE); // plotSScreen
    this.lcd = new Uint8Array(BUFSIZE);  // LCD driver RAM
    this.lcdOn = true;
    this.clock = new Clock(model, realtime);
    this.keys = new Keypad();
    this.curRow = 0; this.curCol = 0; this.penCol = 0; this.penRow = 0;
    this.onFrame = null; this.onWaitKey = null;
    this.frames = 0;
    this.rng = new PCG(seed, seed ^ 0x9E3779B97F4A7C15n);
    this.quitting = false;
  }
  quit() { this.quitting = true; }
  checkQuit() { if (this.quitting) throw new QuitSignal(); }
  // Run executes program (a generator) and swallows QuitSignal.
  *run(program) {
    try { yield* program(); } catch (e) { if (!(e instanceof QuitSignal)) throw e; }
  }

  // ---- timing ----
  spend(n) { this.clock.spend(n); }
  *halt() { this.checkQuit(); this.clock.halt(); yield; this.checkQuit(); }

  // ---- Ion / MirageOS library ----
  fastCopy() { // iFastCopy
    this.spend(CostFastCopy);
    this.lcd.set(this.gbuf);
    this.frames++;
    if (this.onFrame) this.onFrame(this.frames);
  }
  // iGetPix + "ld a,(hl)": the whole byte containing (x,y).
  getPixByte(x, y) {
    this.spend(CostGetPix);
    const i = y * BPR + (x >> 3);
    return i >= BUFSIZE ? 0 : this.gbuf[i];
  }
  // iSprite (ionPutSprite): XOR, 8 wide, no clipping.
  sprite(x, y, rows) {
    this.spend(CostSprite);
    const shift = x % 8; let i = y * BPR + (x >> 3);
    for (const s of rows) {
      if (i < BUFSIZE) this.gbuf[i] ^= s >> shift;
      if (i + 1 < BUFSIZE) this.gbuf[i + 1] ^= (s << (8 - shift)) & 0xFF;
      i += BPR;
    }
  }
  random(n) { this.spend(CostRandom); return this.rng.intN(n); } // iRandom
  grBufClr() { this.spend(4000); this.gbuf.fill(0); }            // _GrBufClr

  // ---- keypad ----
  getCSC() { this.checkQuit(); this.spend(CostGetCSC); return this.keys.pop(); } // _GetCSC
  *waitCSC() {
    if (this.onWaitKey) this.onWaitKey();
    for (;;) {
      const k = this.getCSC();
      if (k !== 0) { this.clock.resync(); return k; }
      yield* this.halt();
    }
  }
  // ArrowPort: keypad group $FE, active low: 0 down, 1 left, 2 right, 3 up.
  arrowPort() {
    this.spend(CostKeyPort);
    let v = 0xFF;
    [SkDown, SkLeft, SkRight, SkUp].forEach((sc, bit) => { if (this.keys.isHeld(sc)) v &= ~(1 << bit); });
    return v & 0xFF;
  }
  // PowerOff: [MODE] does "ld a,1 / out (3),a / halt" until [ON].
  *powerOff() {
    this.lcdOn = false;
    this.keys.clearOn();
    while (!this.keys.takeOn()) { this.checkQuit(); yield SLEEP; }
    this.keys.drain();
    this.lcdOn = true;
    this.clock.resync();
  }

  // ---- calc/text.go: TI-OS display calls draw straight to the LCD ----
  lcdSet(x, y, on) {
    if (x < 0 || x >= W || y < 0 || y >= H) return;
    const i = y * BPR + (x >> 3), mask = 0x80 >> (x % 8);
    if (on) this.lcd[i] |= mask; else this.lcd[i] &= ~mask;
  }
  clrLCDFull() { this.spend(8000); this.lcd.fill(0); } // _ClrLCDFull
  homeUp() { this.curRow = 0; this.curCol = 0; }       // _HomeUp
  setCursor(row, col) { this.curRow = row; this.curCol = col; }
  setPen(col, row) { this.penCol = col; this.penRow = row; }
  putC(ch) { // _PutC: large font, wraps after column 15
    this.spend(1500);
    const g = (LARGE_FONT[ch] || LARGE_FONT['?']).split('|');
    const x0 = this.curCol * 6, y0 = this.curRow * 8;
    for (let y = 0; y < 8; y++)
      for (let x = 0; x < 6; x++)
        this.lcdSet(x0 + x, y0 + y, y < 7 && x > 0 && g[y][x - 1] === '#');
    this.curCol++;
    if (this.curCol >= 16) { this.curCol = 0; this.advanceRow(); }
  }
  putS(s) { for (const ch of s) this.putC(ch); } // _PutS
  newLine() { this.curCol = 0; this.advanceRow(); } // _NewLine
  advanceRow() { if (this.curRow < 7) this.curRow++; }
  dispHL(v) { this.putS(String(v).padStart(5, ' ')); } // _DispHL
  vPutS(s) { // _VPutS: small font at the pen
    for (const ch of s) {
      const g = (SMALL_FONT[ch] || SMALL_FONT['?']).split('|');
      const w = g[0].length;
      if (this.penCol + w + 1 > W) return;
      this.spend(1200);
      for (let y = 0; y < 6; y++)
        for (let x = 0; x <= w; x++)
          this.lcdSet(this.penCol + x, this.penRow + y, x > 0 && y < g.length && g[y][x - 1] === '#');
      this.penCol += w + 1;
    }
  }
  dispOP1A(v, max) { this.vPutS(String(v).slice(0, max)); } // _DispOP1A
  // _DarkLine: TI graph coordinates, (0,0) at the bottom left.
  darkLine(x1, y1, x2, y2) {
    this.spend(6000);
    y1 = H - 1 - y1; y2 = H - 1 - y2;
    const dx = Math.abs(x2 - x1), dy = -Math.abs(y2 - y1);
    const sx = Math.sign(x2 - x1), sy = Math.sign(y2 - y1);
    let err = dx + dy;
    for (;;) {
      this.lcdSet(x1, y1, true);
      if (x1 === x2 && y1 === y2) return;
      const e2 = 2 * err;
      if (e2 >= dy) { err += dy; x1 += sx; }
      if (e2 <= dx) { err += dx; y1 += sy; }
    }
  }
}

// ---- game/variant.go ----
const V15 = { version: '1.5', author: 'By AE, FD, IG', multiplayer: false, defaultSpeed: 1, scoreLabel: 'Score:', extraExitKeys: false };
const V165 = { version: '1.65', author: 'By AE, FD, IG, MB', multiplayer: true, defaultSpeed: 2, scoreLabel: 'Your Score:', extraExitKeys: true };
const variantByVersion = v => ({ '1.5': V15, 'v1.5': V15, '15': V15, '1.65': V165, 'v1.65': V165, '165': V165 })[v];

// ---- game/store_js.go: same localStorage key and JSON shape ----
const STORAGE_KEY = 'falldown-settings';
const localStore = {
  read() { try { return JSON.parse(localStorage.getItem(STORAGE_KEY)) || {}; } catch (e) { return {}; } },
  load(version) { const s = this.read()[version]; return s ? s : null; },
  save(version, set) {
    try { const all = this.read(); all[version] = { high: set.high, speed: set.speed, players: set.players }; localStorage.setItem(STORAGE_KEY, JSON.stringify(all)); } catch (e) {}
  },
};

// ---- game/falldown.go ----
const brick = [0b11111111, 0b10000001, 0b10111111, 0b11111111];      // block:
const ballSprite = [0b01111000, 0b10110100, 0b11001100, 0b11001100, 0b10110100, 0b01111000]; // ball:

// rowInterval: 24 - score/128 in 8 bits (0 means 256).
const rowInterval = score => (24 - ((((score * 2) & 0xFFFF) >> 8) & 0xFF)) & 0xFF;

class Game {
  constructor(m, v, store) {
    this.m = m; this.v = v; this.store = store;
    this.set = { high: 0, speed: v.defaultSpeed, players: 1 };
    if (store) { const s = store.load(v.version); if (s) this.set = { high: (s.high | 0) & 0xFFFF, speed: s.speed | 0, players: s.players | 0 }; }
    if (this.set.speed !== 1 && this.set.speed !== 2) this.set.speed = v.defaultSpeed;
    if (this.set.players !== 2 || !v.multiplayer) this.set.players = 1;
    this.me = { x: 44, y: 0, score: 0 }; // ballx / bally / score
  }
  // Run plays until the player quits from the title screen.
  *run() { const self = this; yield* this.m.run(function* () { while (yield* self.title()) yield* self.play(); }); }
  ball() { return [this.me.x, this.me.y]; }
  score() { return this.me.score; }
  twoPlayer() { return this.set.players === 2; }
  save() { if (this.store) this.store.save(this.v.version, this.set); }

  // title is the "start:" screen. Returns false when the player quits.
  *title() {
    const m = this.m;
    for (;;) {
      m.clrLCDFull();
      m.setCursor(0, 4);
      m.putS('Falldown');
      m.darkLine(0, 55, 94, 55);
      m.setPen(1, 10);
      m.vPutS(this.v.author);
      m.setPen(4, 22);
      m.vPutS('2nd - Start');
      let speedRow = 29;
      if (this.v.multiplayer) { m.setPen(4, 29); m.vPutS('Right - Players: '); speedRow = 36; }
      m.setPen(4, speedRow);
      m.vPutS('ALPHA - Speed:');
      m.setPen(4, speedRow + 7);
      m.vPutS('CLEAR - Exit');
      m.setPen(1, 55);
      m.vPutS('High: ');
      m.dispOP1A(this.set.high, 5);
      const updatm = () => {
        m.setPen(53, speedRow);
        m.dispOP1A(this.set.speed, 1);
        if (this.v.multiplayer) { m.setPen(56, 29); m.dispOP1A(this.set.players, 1); }
      };
      updatm();
      menul: for (;;) {
        switch (yield* m.waitCSC()) {
          case SkClear: return false;
          case SkMode: if (this.v.extraExitKeys) return false; break;
          case SkAlpha: this.set.speed ^= 0b11; this.save(); updatm(); break; // toggle 1 <-> 2
          case SkRight: if (this.v.multiplayer) { this.set.players ^= 0b11; this.save(); updatm(); } break;
          case Sk2nd:
            if (!this.twoPlayer()) return true;
            if (yield* this.waitForPartner()) return true;
            break menul; // cancelled: redraw the title
        }
      }
    }
  }

  // waitForPartner: this port has no link cable (2-player is desktop-only),
  // so it shows "Waiting..." / "Link error" and waits for a key, like the
  // wasm build with no cable (minus its FastCopy that blanks the message).
  *waitForPartner() {
    const m = this.m;
    m.grBufClr();
    m.fastCopy();
    m.setCursor(0, 0);
    m.putS('Waiting...');
    m.newLine();          // linkError:
    m.putS('Link error');
    m.newLine();
    m.putS('2-player needs');
    m.newLine();
    m.putS('the desktop app');
    yield* m.waitCSC();
    return false;
  }

  // play is "begin:" through the end of the score screen.
  *play() {
    const m = this.m;
    m.grBufClr();
    this.me = { x: 44, y: 0, score: 0 };
    for (;;) { // init:
      this.randomblocks();
      // The rows come closer together as the score climbs.
      let blockcnt = rowInterval(this.me.score);
      for (;;) { // sloop:
        this.scroll();
        if (this.checkcoll(this.me)) { yield* this.scoreScreen(); return; }
        this.putball();
        m.fastCopy();
        this.putball();
        for (let i = 0; i < this.set.speed; i++) yield* m.halt(); // delay: halt / djnz delay
        if (!(yield* this.move())) { yield* this.exit(); return; }
        m.spend(CostMisc);
        blockcnt = (blockcnt - 1) & 0xFF;
        if (blockcnt === 0) break;
      }
    }
  }

  // move reads input (single player). False ends the game via "exit:".
  *move() {
    const m = this.m;
    this.ckkey();
    switch (m.getCSC()) {
      case SkClear: return false;
      case SkMode: yield* m.powerOff(); break; // "pause": off until [ON]
    }
    return true;
  }

  // ckkey: arrow keys straight from the keypad port, every frame.
  ckkey() {
    const port = this.m.arrowPort(), me = this.me;
    if ((port & 0b0010) === 0) { // [(left)]
      if (me.x < 2) return; // offscreen
      this.xcheck(me.x - 2, me.x - 2);
    } else if ((port & 0b0100) === 0) { // [(right)]
      const tmpx = (me.x + 2) & 0xFF;
      if (tmpx >= 96 - 4) return; // offscreen
      this.xcheck(tmpx, tmpx + 5);
    }
  }

  // xcheck: move unless the byte at column px is occupied on the ball's
  // top or bottom row. Only those two rows are tested.
  xcheck(tmpx, px) {
    if (this.m.getPixByte(px, this.me.y) !== 0) return; // something in way
    if (this.m.getPixByte(px, (this.me.y + 5) & 0xFF) !== 0) return;
    this.me.x = tmpx;
  }

  // randomblocks: a new row of bricks at rows 60-63.
  randomblocks() {
    const m = this.m, row = 60 * BPR;
    for (let i = 0; i < BPR; i++) { // 12 blocks
      const a = m.random(4); // one chance in 4 of a hole
      if (a === 0) continue; // skip space
      m.spend(CostBlockDraw);
      for (let r = 0; r < brick.length; r++) m.gbuf[row + i + r * BPR] = brick[r];
    }
    // The forced hole: the original's loop tested the first brick twelve
    // times, so a hole is carved whenever column 0 is a brick.
    if (this.v.multiplayer) { if (m.gbuf[row] === 0) return; }
    else if ((m.gbuf[row] & 1) === 0) return;
    const a = m.random(12); // put random space
    for (let r = 0; r < 4; r++) { const i = row + a + r * BPR; if (i < BUFSIZE) m.gbuf[i] = 0; }
  }

  // scroll: the buffer up one pixel (ldir of 756 bytes), clear the bottom row.
  scroll() {
    const b = this.m.gbuf;
    this.m.spend(CostScroll);
    b.copyWithin(0, BPR);
    b.fill(0, BUFSIZE - BPR);
  }

  // checkcoll: fall, rest on a shelf, or get pushed up. True = off the top.
  checkcoll(b) {
    const m = this.m;
    if (m.getPixByte(b.x, b.y + 6) === 0 && m.getPixByte(b.x + 5, b.y + 6) === 0) {
      b.score = (b.score + 1) & 0xFFFF; // inc score
      b.y++;                              // move the ball down
      if (b.y >= 60 - 5) b.y--;
      return false;
    }
    // ndown:
    if (m.getPixByte(b.x, b.y + 5) === 0 && m.getPixByte(b.x + 5, b.y + 5) === 0) return false;
    b.y = (b.y - 1) & 0xFF; // moveup:
    return (b.y & 0x80) !== 0;
  }

  putball() { this.m.sprite(this.me.x, this.me.y, ballSprite); }

  *exit() { yield* this.scoreScreen(); } // exit: (no link to tell)

  // scoreScreen is "nolsnd:" through "exitl:".
  *scoreScreen() {
    const m = this.m;
    m.clrLCDFull();
    m.homeUp();
    m.putS(this.v.scoreLabel);
    m.dispHL(this.me.score);
    // cphlde / jr c: a tie with the old high counts as a new high.
    if (this.me.score >= this.set.high) {
      this.set.high = this.me.score;
      this.save();
      m.newLine();
      m.putS('New High!');
    }
    for (;;) {
      switch (yield* m.waitCSC()) {
        case Sk2nd: return;
        case SkClear: case SkMode: if (this.v.extraExitKeys) return; break;
      }
    }
  }
}

// autopilot/autopilot.go (tests only).
function autoSteer(buf, x, y) {
  let shelf = -1;
  for (let r = y + 6; r < H && shelf < 0; r++)
    for (let c = 0; c < BPR; c++) if (buf[r * BPR + c] !== 0) { shelf = r; break; }
  if (shelf < 0) return [false, false];
  let best = -1, bestDist = 1 << 30;
  for (let c = 0; c < BPR; c++) {
    if (buf[shelf * BPR + c] !== 0) continue;
    for (const t of [c * 8, c * 8 + 2]) {
      if (t > 90) continue;
      const d = Math.abs(t - x);
      if (d < bestDist) { best = t; bestDist = d; }
    }
  }
  if (best < 0) return [false, false];
  if (best < x) return [true, false];
  if (best > x) return [false, true];
  return [false, false];
}
