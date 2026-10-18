# Porting Notes

How the TI-83 Plus original works and what the port preserves. The Z80 source isn't in the repository; `scripts/fetch-originals.sh` downloads it into [`original/`](../original/README.md). Read `original/v1.65-multiplayer/fdown.asm` alongside [`internal/game/falldown.go`](../internal/game/falldown.go), whose comments use the same labels.

## Contents

- [Lineage](#lineage)
- [How the game works](#how-the-game-works)
- [Timing](#timing)
- [Preserved quirks](#preserved-quirks)
- [Two-player protocol](#two-player-protocol)
- [What is approximated](#what-is-approximated)

## Lineage

| Release | Date | Authors | Platform | ticalc.org file |
|---|---|---|---|---|
| FallDown 1.0–1.3 | 1998 | Florent Dhordain, Ahmed El-Helw | TI-82 / TI-83 (CrASH, AShell83, SOS) | `fdown82.asm`, `fdown83.asm` |
| 1.5 | 1999-11-03 | Ian Graf (port) | Ion / MirageOS, TI-83 Plus | `83plus/asm/games/mirageos/fdown.zip` (uploaded 2001 by Peter Jedraszczak) |
| 1.65 | 2006-08-20 | Michael Bruno (multiplayer) | MirageOS, TI-83 Plus / SE / TI-84 Plus | `83plus/asm/games/mirageos/fdownm.zip` |

None of the archives include a license; they were released as freeware with source.

## How the game works

The whole playfield lives in the 768-byte graph buffer (96x64 pixels, 1 bit each, 12 bytes per row). Each frame (`sloop`):

1. **scroll**: shift the buffer up one pixel (`ldir` of 756 bytes) and clear the bottom row.
2. **checkcoll**: look at the bytes under the ball's left and right edges. If both are empty, the ball falls a pixel and scores a point. If the scroll pushed a brick into the ball's bottom row, the ball moves up a pixel. If it goes above row 0, the game is over.
3. **putball / iFastCopy / putball**: XOR the ball in, copy the buffer to the LCD, and XOR it back out, so the buffer only ever holds bricks.
4. **delay**: `halt` once (speed 1) or twice (speed 2).
5. **ckkey**: read the arrow keys directly from keypad port 1. Each frame the ball moves 2 pixels if the destination bytes are clear.
6. **\_GetCSC**: CLEAR ends the game and MODE turns the calculator off.

Every `24 - score/128` frames, **randomblocks** draws a new row of twelve 8x4 bricks at rows 60–63. Each slot has a 1-in-4 chance of being a hole.

The ball is 6x6 pixels and starts at x=44, and every move is 2 pixels, so x is always even.

## Timing

The game has no frame counter or timer. Its speed comes entirely from `halt`, which sleeps until the next hardware interrupt.

- The TI-83 Plus OS timer fires about **120 times a second**. The TI-83 Plus SE and TI-84 Plus fire about 110 times.
- One frame's work at 6 MHz is about 77,000 T-states (12.8 ms):
  - scroll: ~16k
  - MirageOS `iFastCopy`, which waits on the LCD driver: ~52–60k
  - sprites, collision, the key read and the interrupt handler: the rest
- One interrupt period is 50,000 T-states, so the work always crosses one interrupt and the first `halt` waits for the second.

| Model | Speed 1 (1 halt) | Speed 2 (2 halts) |
|---|---|---|
| TI-83 Plus | 2 periods = **60 fps** | 3 periods = **40 fps** |
| TI-83 Plus SE / TI-84 Plus | 2 periods = **55 fps** | 3 periods = **36.7 fps** |

The port runs the game on a virtual Z80 clock ([`internal/calc/clock.go`](../internal/calc/clock.go)). Each routine charges its estimated T-states, and `Halt` advances to the next interrupt boundary and sleeps in real time until then. The cycle estimates only need to land within a few thousand T-states, because the result depends only on how many interrupt boundaries a frame crosses. `TestFrameTiming` checks the frame rates against the virtual clock and `TestRealtimePacing` checks them against the wall clock.

**Input latency.** The original reads the keypad port once per frame, right after the `halt`, so a press shows on the next frame. The port reads key state at the same point. The window samples the keyboard at 240 Hz, so it adds at most about 4 ms. `TestInputIsPerFrame` checks the 2-pixel-per-frame response.

## Preserved quirks

- **Byte-granular collision.** `iGetPix` returns the buffer byte containing a pixel, and the game tests that whole byte for zero. A brick slot is one byte, so the ball slips through a single 8-pixel hole only from x%8 = 0 or 2 (`TestHoleFitIsByteGranular`).
- **Two-row side checks.** Moving sideways tests only the ball's top and bottom rows. Halfway through a shelf, the ball can slide sideways through bricks.
- **The forced hole.** The original meant to add a hole only when a row came out solid. Its counting loop tested the first slot twelve times, so a random slot is cleared whenever slot 0 is a brick. v1.65's author documented this and kept the behavior.
- **Rows bunch up.** The row interval is computed in 8 bits as `24 - score/128`. By score ~2,900 a row arrives every frame and the rows fuse into walls. At 3,072 the value wraps to 0, which means 256 frames between rows (`TestRowInterval`).
- **"New High!" on a tie.** `cphlde` / `jr c` treats an equal score as a new high, so a first game that scores 0 still says "New High!".
- **MODE is power-off.** "Pause" turns the LCD off with `out (3),a` and `halt`, and only ON brings it back.
- **The 2-player off-by-one.** Each calculator checks its own ball first. When one player dies, that calculator skips the other ball's check that frame, so the survivor's score can read one lower on the loser's screen. The v1.65 readme lists this under BUGS, and `TestTwoPlayerLockstep` expects it.

## Two-player protocol

v1.65 sends single bytes over the link port, in lockstep:

- **Bricks.** Player 1 rolls each slot and sends it (0–3, then 0–11 for the forced hole). Player 2 receives them instead of rolling.
- **Movement.** Each frame, player 1 moves and sends its x position, then receives player 2's. Player 2 does the same in the opposite order.
- **Simulation.** Only x positions travel. Both calculators run collision for both balls, so y positions and scores stay in sync deterministically.
- **Row timing.** Player 2 times new rows from player 1's score, as computed locally.
- **Quitting.** CLEAR or MODE while waiting for a byte sends 100 (`LEAVE`).

The port sends the same bytes over TCP. The host is player 1, matching the original, where the calculator already waiting became player 1. One addition: the host listens only while it is waiting, sends a `0xFD` ready byte when it accepts, and the guest echoes it back. A guest that connected through the kernel's accept backlog doesn't start before the host presses 2nd, and a connection that doesn't echo within 2 seconds (one abandoned in the backlog, or anything else that reached the port) is dropped. Each game uses a fresh connection, so stale bytes never leak into the next game.

## What is approximated

- **Fonts.** The TI-OS large and small fonts live in ROM. These are hand-drawn lookalikes. The small font's widths are pinned so the title screen's hard-coded positions line up (the digits at x=53 and x=56).
- **Random numbers.** `iRandom` mixes the Z80's R register, so its sequence can't be reproduced. The port draws uniform values over the same ranges.
- **Text wrapping.** On v1.65's score screen, `_PutC` wraps after column 15, which leaves a blank line after "Your Score:". This follows the documented TI-OS behavior and hasn't been checked against a real calculator.
- **Link timing.** Each byte sent costs a fixed 6,000 T-states. Real TIOS link transfers varied.
- **Key queue.** `_GetCSC` on the calculator holds one pending scan code. The port's keypad keeps up to two, so a quick double tap at a menu isn't lost.
- **Link errors.** A peer that stops reading for 2 seconds ends the game, and a link that can't be set up (port in use, host unreachable) shows "Link error" on the LCD. The cable had no such failures.
