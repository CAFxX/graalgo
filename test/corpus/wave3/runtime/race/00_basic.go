package main

// L11 (REVIEW-2026-11-18-05): runtime/race — zero corpus imports
// before this program. The race package has no public interface:
// importing it is the whole program (it arms the race detector's
// hooks when the binary is built with -race). The pin is the import
// itself plus a trivial deterministic computation.
// (Marked .scopelimited: the graalgo frontend has no race detector;
// retest when it lands.)

import (
	"fmt"
	_ "runtime/race"
)

func main() {
	fmt.Printf("race-imported=true\n")
	// A racy-looking but actually synchronized increment: the
	// program's own behaviour is deterministic by construction.
	sum := 0
	for i := 0; i < 10; i++ {
		sum += i
	}
	fmt.Printf("sum=%d\n", sum)
}
