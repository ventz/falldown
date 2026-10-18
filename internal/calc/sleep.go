//go:build !js

package calc

import "time"

// preciseSleep sleeps for d, finishing the last stretch with a short spin
// so frame pacing doesn't pick up the OS timer's millisecond jitter.
func preciseSleep(d time.Duration) {
	deadline := time.Now().Add(d)
	if d > 2*time.Millisecond {
		time.Sleep(d - 1500*time.Microsecond)
	}
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Microsecond)
	}
}
