package main

import "time"

// tc01: Duration arithmetic and String. All deterministic.
// (ParseDuration is multi-result — not in M5 scope, tested separately.)
func main() {
	d := 5 * time.Second
	println(d.String())
	println(d.Nanoseconds())
	println(d.Milliseconds())
	println(int64(d.Seconds()))
	h := 2 * time.Hour
	println(h.String())
	println(h.Minutes())
	sum := d + h
	println(sum.String())
	diff := h - d
	println(diff.String())
	println((90 * time.Minute).Round(time.Hour).String())
	println((90 * time.Minute).Truncate(time.Hour).String())
	println((-3 * time.Second).Abs().String())
}
