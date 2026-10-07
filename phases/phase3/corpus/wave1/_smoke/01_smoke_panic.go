package main

import "time"

// _smoke 01: panic path — exercises the <base>.panic first-stderr-line
// comparison. Deterministic message; the synthetic time import proves
// multi-package programs work through the harness.
func main() {
	d := 3 * time.Second
	if d == 3*time.Second {
		panic("smoke-boom")
	}
}
