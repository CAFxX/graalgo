package main

import (
	"fmt"
	"time"
)

// tm16 (RV-P1-22): the FIRST value-sensitive corpus case — asserts the
// wall-clock VALUE delivered on timer channels, not just event order.
// The Time received on <-time.After / Ticker.C must be the fire instant:
// within [before+300ms-50ms, after+50ms] for the one-shot, and
// non-decreasing across ticker ticks. (P1-2 margins: 300ms separations,
// 50ms tolerance.)
func main() {
	before := time.Now()
	v := <-time.After(300 * time.Millisecond)
	after := time.Now()
	if v.Before(before.Add(250*time.Millisecond)) || v.After(after.Add(50*time.Millisecond)) {
		fmt.Println("FAIL: fire value out of bounds", v, "before:", before, "after:", after)
		return
	}
	fmt.Println("fire value ok")

	tk := time.NewTicker(300 * time.Millisecond)
	prev := <-tk.C
	ok := true
	for i := 0; i < 2; i++ {
		cur := <-tk.C
		if cur.Before(prev) {
			ok = false
			break
		}
		prev = cur
	}
	tk.Stop()
	if !ok {
		fmt.Println("FAIL: ticks not non-decreasing")
		return
	}
	fmt.Println("ticks ok")
}
