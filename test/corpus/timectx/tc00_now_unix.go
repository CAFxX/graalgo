package main

import "time"

// tc00: time.Now basics — Unix/UnixNano/IsZero/ordering. Prints properties,
// not absolute times (differential-safe: gc and graalgo run at different
// wall-clock times).
func main() {
	t := time.Now()
	if t.IsZero() {
		println("zero")
	} else {
		println("non-zero")
	}
	if t.Unix() > 0 {
		println("unix-positive")
	}
	if t.UnixNano() > 0 {
		println("unixnano-positive")
	}
	t2 := time.Now()
	if t2.After(t) || t2.Equal(t) {
		println("monotonic")
	}
	if t2.Sub(t) >= 0 {
		println("sub-nonnegative")
	}
}
