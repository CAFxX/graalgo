package main

import "time"

// _smoke 01: panic path — exercises the live first-stderr-line comparison
// against the gc leg (plus the .panic anchor drift check). Deterministic
// message; the synthetic time import proves
// multi-package programs work through the harness.
func main() {
	d := 3 * time.Second
	if d == 3*time.Second {
		panic("smoke-boom")
	}
}
