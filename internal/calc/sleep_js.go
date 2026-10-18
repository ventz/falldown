package calc

import "time"

// preciseSleep is a plain sleep in the browser. Every short sleep there is
// a setTimeout, which browsers stretch to 4 ms when nested, so the desktop
// spin would overshoot. Frames are scheduled against absolute times, so the
// jitter doesn't add up and the average speed stays right.
func preciseSleep(d time.Duration) { time.Sleep(d) }
