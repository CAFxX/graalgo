package main

// Loop-capture: deferred closures in a loop capture per-iteration cells
// (the M5 known-limitation case). The inner funclit's defers run at its
// return, before the observe point.
func main() {
	sum := 0
	func() {
		for i := 0; i < 5; i++ {
			defer func() { sum += i }()
		}
	}()
	_ = sum
}

// observe: sum
