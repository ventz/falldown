# Original Releases

The Go port is translated from the original Z80 source of Fall Down. Those releases belong to their authors, carry no license of their own, and are not included in this repository. Fetch them from [ticalc.org](https://www.ticalc.org/) into this folder with:

```bash
scripts/fetch-originals.sh
```

The script checks each download against a known SHA-256 sum and unpacks it:

| Directory | Release | Authors | ticalc.org file |
|---|---|---|---|
| `v1.5-ion-mirageos/` | 1.5 (1999) | Ian Graf (port); Florent Dhordain and Ahmed El-Helw (original) | [`fdown.zip`](https://www.ticalc.org/pub/83plus/asm/games/mirageos/fdown.zip) |
| `v1.65-multiplayer/` | 1.65 (2006) | Michael Bruno (multiplayer); Ian Graf; Florent Dhordain and Ahmed El-Helw | [`fdownm.zip`](https://www.ticalc.org/pub/83plus/asm/games/mirageos/fdownm.zip) |

The port's comments use the same labels as `v1.65-multiplayer/fdown.asm`, so the two read side by side. Nothing here is needed to build or play the game.
