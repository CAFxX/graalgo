package main

// fix-redmaster-r53 (review-redmaster-r52 N1): regression corpus for the
// r52 SchedMain ServiceLoader registration. An interface method call
// emits the "ifacemethod" wire kind, which only the Phase-3 provider
// table registers; without the r52 block SchedMain dies building the
// AST with "unsupported expression kind ifacemethod" (the reviewer's
// negative control). The runtime subprocess tests run through
// SchedMain, so this program — not the snapshot corpus, which runs
// through P3SyncMain — is what pins the registration. Expected output
// recorded with gc 1.27.1 (go run).

import "fmt"

type I interface{ M() int }

type T struct{}

func (T) M() int { return 42 }

func main() {
	var i I = T{}
	fmt.Println(i.M())
}
