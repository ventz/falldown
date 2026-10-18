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
  inputTick(t);
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
  for (const sc in applied) if (applied[sc]) machine.keys.held[sc] = true;
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
// Like the Go app, key events only record what is down; the emulated keypad
// is updated from that state in a fixed-rate input tick (ui.go: Update at
// TPS = 240, run from the browser's animation frame the way Ebitengine does
// on the web). The game itself reads the arrow keys once per frame from the
// keypad port (machine.arrowPort), so held keys behave exactly as in Go and
// nothing samples faster than the original.
const TPS = 240, TICK_MS = 1000 / TPS;
const holders = {};          // scan code -> Set of sources holding it
const applied = {};          // scan code -> state the keypad last saw
const tapped = new Set();    // went down since the last tick (a tap shorter than a tick still registers)
let onTapped = false, tickAcc = 0, lastTickT = 0;
function setHeld(src, sc, down) {
  const s = holders[sc] || (holders[sc] = new Set());
  if (down) { if (!s.has(src)) { s.add(src); tapped.add(sc); } } else s.delete(src);
}
function pressOn() { onTapped = true; }
function releaseAll() { for (const sc in holders) holders[sc].clear(); tapped.clear(); inputUpdate(); }
// ui.go App.Update: push changed key states to the keypad.
function inputUpdate() {
  for (const k in holders) {
    const sc = +k, down = holders[sc].size > 0 || tapped.has(sc);
    if (down === !!applied[sc]) continue;
    applied[sc] = down;
    if (!machine) continue;
    if (down) {
      if (done) { if (sc === Sk2nd) startProgram(); continue; }
      machine.keys.press(sc);
    } else machine.keys.release(sc);
  }
  tapped.clear();
  if (onTapped) { onTapped = false; if (machine) machine.keys.pressOn(); }
}
// Fixed timestep: at most one input update per 1/240 s of elapsed time,
// whatever the display refresh rate (60, 120, 144 Hz...).
function inputTick(t) {
  if (!lastTickT) lastTickT = t;
  tickAcc = Math.min(tickAcc + (t - lastTickT), 250); lastTickT = t;
  if (tickAcc < TICK_MS) return;
  tickAcc %= TICK_MS;
  inputUpdate();
}

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

// ---- touch controls ----
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
for (const el of [canvas, $('touch'), ...document.querySelectorAll('.tb')])
  for (const ev of ['touchstart', 'touchmove', 'touchend']) el.addEventListener(ev, block, { passive: false });

// Calculator keys. Press-and-hold is holding the key: down on pointerdown,
// up on pointerup / pointercancel / pointerleave. Each finger is its own
// source, so LEFT and RIGHT can be held together and multi-touch works
// (with both held the game moves left, as ckkey tests left first). Pointer
// capture is released so sliding off a key lets it go, and sliding onto
// the other arrow presses that one.
for (const el of document.querySelectorAll('.tb')) {
  const sc = +el.dataset.sc, fingers = new Set();
  const sync = () => el.classList.toggle('down', fingers.size > 0);
  const down = id => {
    if (fingers.has(id)) return;
    fingers.add(id); sync();
    if (sc) setHeld('btn:' + el.id + ':' + id, sc, true); else pressOn();
  };
  const up = id => {
    if (!fingers.delete(id)) return;
    sync();
    if (sc) setHeld('btn:' + el.id + ':' + id, sc, false);
  };
  el.addEventListener('pointerdown', e => {
    e.preventDefault();
    try { if (el.hasPointerCapture(e.pointerId)) el.releasePointerCapture(e.pointerId); } catch (_) {}
    down('p' + e.pointerId);
  });
  el.addEventListener('pointerenter', e => { if (e.buttons & 1) down('p' + e.pointerId); });
  for (const ev of ['pointerup', 'pointercancel', 'pointerleave']) el.addEventListener(ev, e => up('p' + e.pointerId));
  el.addEventListener('contextmenu', block);
  if (window.PointerEvent) continue;
  // Fallback for browsers without pointer events: each finger on the key
  // holds it until it lifts.
  el.addEventListener('touchstart', e => { for (const t of e.changedTouches) down('t' + t.identifier); }, { passive: true });
  const tEnd = e => { for (const t of e.changedTouches) up('t' + t.identifier); };
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
window.falldown = { get machine() { return machine; }, get game() { return game; }, TPS };
