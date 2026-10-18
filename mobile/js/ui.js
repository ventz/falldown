// ===== Page: LCD view (internal/lcdview), keyboard/touch (internal/ui), driver =====
const params = new URLSearchParams(location.search);
const $ = id => document.getElementById(id);
const isTouch = matchMedia('(pointer: coarse)').matches || 'ontouchstart' in window || params.has('touch');
if (isTouch) document.body.classList.add('touch');

const VARIANT = variantByVersion(params.get('v') || params.get('version') || '1.65') || V165;
const MODEL = MODELS[params.get('model') === '84p' ? '84p' : '83p'];
const SEED = /^\d+$/.test(params.get('seed') || '') ? BigInt(params.get('seed')) : 0;

// ---- lcdview.go: grey-green glass, faint pixel grid, optional ghosting ----
const GLASS = [0x9C, 0xA8, 0x8A], UNLIT = [0x95, 0xA1, 0x83], LIT = [0x1C, 0x22, 0x1A];
let ghostMs = params.has('ghost') ? Math.max(0, +params.get('ghost') || 0) : 12; // -ghost 12ms
const level = new Float32Array(W * H);
let lastDraw = 0;
const canvas = $('lcd'), ctx2 = canvas.getContext('2d');
const small = document.createElement('canvas'); small.width = W; small.height = H;
const sctx = small.getContext('2d'), img = sctx.createImageData(W, H);
let scale = 0, pad = 0, grid = null;

function fit() {
  const box = $('screen'), dpr = window.devicePixelRatio || 1;
  const bw = box.clientWidth * dpr, bh = box.clientHeight * dpr;
  // Largest whole-number scale (ui.go), counting the glass border pad = 2*scale.
  const s = Math.max(1, Math.floor(Math.min(bw / (W + 4), bh / (H + 4))));
  if (s === scale) return;
  scale = s; pad = Math.max(s * 2, 4);
  canvas.width = W * s + 2 * pad; canvas.height = H * s + 2 * pad;
  canvas.style.width = canvas.width / dpr + 'px'; canvas.style.height = canvas.height / dpr + 'px';
  // GridMask: glass-colored lines between pixels.
  const gap = Math.floor(s / 7);
  grid = document.createElement('canvas'); grid.width = W * s; grid.height = H * s;
  if (gap > 0) {
    const g = grid.getContext('2d'); g.fillStyle = `rgb(${GLASS})`;
    for (let x = 0; x < W; x++) g.fillRect(x * s, 0, gap, H * s);
    for (let y = 0; y < H; y++) g.fillRect(0, y * s, W * s, gap);
  }
  lastSig = '';
}

let lastSig = '', still = 0;
function draw(t) {
  requestAnimationFrame(draw);
  const m = machine; if (!m || !scale) return;
  const sig = m.lcdOn + String.fromCharCode.apply(null, m.lcd);
  if (sig === lastSig && still > 60) { lastDraw = t; return; } // nothing moving
  still = sig === lastSig ? still + 1 : 0; lastSig = sig;
  // View.Update
  let alpha = 1;
  if (ghostMs > 0 && lastDraw) alpha = 1 - Math.exp(-Math.min(t - lastDraw, 200) / ghostMs);
  lastDraw = t;
  const d = img.data;
  for (let y = 0, i = 0; y < H; y++) for (let x = 0; x < W; x++, i++) {
    const target = m.lcdOn && (m.lcd[y * BPR + (x >> 3)] & (0x80 >> (x & 7))) ? 1 : 0;
    const l = (level[i] += (target - level[i]) * alpha);
    // View.RenderSmall
    for (let k = 0; k < 3; k++) d[i * 4 + k] = m.lcdOn ? UNLIT[k] + (LIT[k] - UNLIT[k]) * l : GLASS[k];
    d[i * 4 + 3] = 255;
  }
  sctx.putImageData(img, 0, 0);
  ctx2.fillStyle = `rgb(${GLASS})`; ctx2.fillRect(0, 0, canvas.width, canvas.height);
  ctx2.imageSmoothingEnabled = false;
  ctx2.drawImage(small, pad, pad, W * scale, H * scale);
  ctx2.drawImage(grid, pad, pad);
}

// ---- driver: runs the game generator against the virtual clock ----
let machine = null, game = null, gen = null, timer = 0, done = false;
function startProgram() {
  machine = new Machine(MODEL, true, SEED);
  game = new Game(machine, VARIANT, localStore);
  gen = game.run(); done = false;
  // Keys still held carry over to the new machine.
  for (const sc in holders) if (holders[sc].size) machine.keys.held[sc] = true;
  machine.clock.resync();
  pump();
}
function pump() {
  clearTimeout(timer);
  for (let i = 0; i < 10000; i++) {
    const r = gen.next();
    if (r.done) { programExited(); return; }
    if (r.value === SLEEP) { timer = setTimeout(pump, 10); return; } // PowerOff poll
    // Clock.sync: sleep until the wall time of the virtual clock.
    const target = machine.clock.wallAt(machine.clock.t), now = performance.now();
    if (target > now) { timer = setTimeout(pump, target - now); return; }
    if (now - target > 50) machine.clock.resync(); // blocked: don't catch up in a burst
  }
  timer = setTimeout(pump, 0);
}
// The game quit from the title (CLEAR, or MODE on v1.65). The desktop app
// closes its window here; this page shows a TI-OS style "Done" and 2nd runs
// the program again.
function programExited() {
  done = true;
  const m = machine; m.clrLCDFull(); m.lcdOn = true;
  m.setCursor(0, 0); m.putS('Falldown');
  m.setCursor(1, 12); m.putS('Done');
  m.setPen(1, 55); m.vPutS('2nd - Run again');
}

// ---- input (ui.go KeyMap), shared by keyboard and touch ----
const holders = {}; // scan code -> Set of sources holding it
function setHeld(src, sc, down) {
  const s = holders[sc] || (holders[sc] = new Set());
  const was = s.size > 0;
  down ? s.add(src) : s.delete(src);
  const is = s.size > 0;
  if (is === was || !machine) return;
  if (is) {
    if (done) { if (sc === Sk2nd) startProgram(); return; }
    machine.keys.press(sc);
  } else machine.keys.release(sc);
}
function pressOn() { if (machine) machine.keys.pressOn(); }
function releaseAll() { for (const sc in holders) for (const src of [...holders[sc]]) setHeld(src, +sc, false); }

const KEYMAP = {
  ArrowLeft: SkLeft, ArrowRight: SkRight, ArrowUp: SkUp, ArrowDown: SkDown,
  Space: Sk2nd, Enter: Sk2nd, NumpadEnter: Sk2nd, KeyZ: Sk2nd,
  KeyA: SkAlpha, Tab: SkAlpha, KeyM: SkMode,
  Escape: SkClear, Backspace: SkClear, KeyQ: SkClear,
};
addEventListener('keydown', e => {
  if (e.ctrlKey || e.metaKey || e.altKey) return;
  const sc = KEYMAP[e.code];
  if (sc) { e.preventDefault(); setHeld('kb:' + e.code, sc, true); return; }
  if (e.repeat) return;
  if (e.code === 'KeyO') pressOn();                       // OnKeys
  else if (e.code === 'KeyG') ghostMs = ghostMs > 0 ? 0 : 12; // G toggles ghosting
});
addEventListener('keyup', e => { const sc = KEYMAP[e.code]; if (sc) setHeld('kb:' + e.code, sc, false); });
addEventListener('blur', releaseAll);
document.addEventListener('visibilitychange', () => { if (document.hidden) releaseAll(); });

// ---- touch controls (modeled on z-axis-traffic) ----
const block = e => { if (e.cancelable) e.preventDefault(); };
for (const ev of ['gesturestart', 'gesturechange', 'gestureend']) document.addEventListener(ev, block, { passive: false });
document.addEventListener('dblclick', block, { passive: false });
let lastTouchEnd = 0;
document.addEventListener('touchend', e => {
  const now = Date.now();
  if (now - lastTouchEnd < 400 && !(e.target.closest && e.target.closest('a'))) block(e);
  lastTouchEnd = now;
}, { passive: false });
document.addEventListener('touchmove', e => { if (e.touches.length > 1 || !(e.target.closest && e.target.closest('footer'))) block(e); }, { passive: false });
for (const el of [canvas, $('touch'), $('stick'), ...document.querySelectorAll('.tb')])
  for (const ev of ['touchstart', 'touchmove', 'touchend']) el.addEventListener(ev, block, { passive: false });

// Thumb stick: steers by finger direction, not distance from the center.
// It holds the arrow key the way the calculator's arrow keys work. Landing
// on one side presses that side at once; after that, a small move the other
// way (FLIP px from the farthest point) switches direction immediately, so a
// left-to-right change never has to travel back through a dead zone.
const FLIP = 4, LAND = 0.12;
const stick = { id: null, x: 0, dir: 0, ext: 0 }, sEl = $('stick'), knob = $('knob');
function stickDir(d) {
  if (d === stick.dir) return;
  stick.dir = d; stick.x = d;
  setHeld('stick', SkLeft, d < 0);
  setHeld('stick', SkRight, d > 0);
}
function stickMove(e, first) {
  const r = sEl.getBoundingClientRect(), R = r.width / 2;
  let dx = e.clientX - r.left - R, dy = e.clientY - r.top - R;
  const l = Math.hypot(dx, dy); if (l > R) { dx *= R / l; dy *= R / l; }
  knob.style.transform = `translate3d(${dx}px,${dy}px,0)`;
  const x = e.clientX;
  if (first) { stick.ext = x; stickDir(dx > R * LAND ? 1 : dx < -R * LAND ? -1 : 0); return; }
  if (stick.dir > 0) { if (x > stick.ext) stick.ext = x; else if (stick.ext - x > FLIP) { stick.ext = x; stickDir(-1); } }
  else if (stick.dir < 0) { if (x < stick.ext) stick.ext = x; else if (x - stick.ext > FLIP) { stick.ext = x; stickDir(1); } }
  else if (Math.abs(x - stick.ext) > FLIP) { stickDir(x > stick.ext ? 1 : -1); stick.ext = x; }
}
function stickEvents(e) { return e.getCoalescedEvents ? e.getCoalescedEvents() : [e]; }
sEl.addEventListener('pointerdown', e => { e.preventDefault(); stick.id = e.pointerId; sEl.setPointerCapture(e.pointerId); stickMove(e, true); });
sEl.addEventListener('pointermove', e => { if (e.pointerId === stick.id) for (const c of stickEvents(e)) stickMove(c.clientX === undefined ? e : c); });
if ('onpointerrawupdate' in window) sEl.addEventListener('pointerrawupdate', e => { if (e.pointerId === stick.id) stickMove(e); });
const stickEnd = e => { if (e.pointerId === stick.id) { stick.id = null; knob.style.transform = ''; stickDir(0); } };
sEl.addEventListener('pointerup', stickEnd); sEl.addEventListener('pointercancel', stickEnd);

for (const el of document.querySelectorAll('.tb')) {
  const src = 'btn:' + el.id, sc = +el.dataset.sc;
  const down = () => { el.classList.add('down'); if (sc) setHeld(src, sc, true); else pressOn(); };
  const up = () => { el.classList.remove('down'); if (sc) setHeld(src, sc, false); };
  el.addEventListener('pointerdown', e => { e.preventDefault(); try { el.setPointerCapture(e.pointerId); } catch (_) {} down(); });
  el.addEventListener('pointerup', up); el.addEventListener('pointercancel', up); el.addEventListener('lostpointercapture', up);
  // iOS fallback: the zoom guard preventDefaults touches here, so drive from touch events too.
  el.addEventListener('touchstart', down, { passive: true });
  const tEnd = e => { if (!e.targetTouches || e.targetTouches.length === 0) up(); };
  el.addEventListener('touchend', tEnd, { passive: true }); el.addEventListener('touchcancel', tEnd, { passive: true });
}
// Tapping the dark LCD after MODE is the same as ON.
canvas.addEventListener('pointerdown', () => { if (machine && !machine.lcdOn) pressOn(); });

addEventListener('resize', fit);
if (window.visualViewport) visualViewport.addEventListener('resize', fit);
fit();
startProgram();
requestAnimationFrame(draw);
// For tests and the curious.
window.falldown = { get machine() { return machine; }, get game() { return game; }, stick };
