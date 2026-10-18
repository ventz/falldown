package game

// Variant selects which release of the TI-83 Plus game to reproduce.
type Variant struct {
	Version      string
	Author       string // credit line on the title screen
	Multiplayer  bool   // v1.65 adds the 2-player link mode
	DefaultSpeed uint8  // halts per frame: 1 is fast, 2 is slow
	ScoreLabel   string // _PutS label on the score screen
	// v1.65 also leaves the score screen on [CLEAR]/[MODE] and quits the
	// title screen on [MODE]; v1.5 only reacts to [2nd] / [CLEAR].
	ExtraExitKeys bool
}

var (
	// V15 is Ian Graf's 1999 Ion/MirageOS port (fdown.zip on ticalc.org,
	// uploaded by Peter Jedraszczak in 2001).
	V15 = Variant{
		Version:      "1.5",
		Author:       "By AE, FD, IG",
		DefaultSpeed: 1,
		ScoreLabel:   "Score:",
	}
	// V165 is Michael Bruno's 2006 multiplayer edition (fdownm.zip).
	V165 = Variant{
		Version:       "1.65",
		Author:        "By AE, FD, IG, MB",
		Multiplayer:   true,
		DefaultSpeed:  2,
		ScoreLabel:    "Your Score:",
		ExtraExitKeys: true,
	}
)

// VariantByVersion looks a variant up by its version string.
func VariantByVersion(v string) (Variant, bool) {
	switch v {
	case "1.5", "v1.5", "15":
		return V15, true
	case "1.65", "v1.65", "165":
		return V165, true
	}
	return Variant{}, false
}
