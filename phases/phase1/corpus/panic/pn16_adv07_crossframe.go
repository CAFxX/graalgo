package main

// R2 adv07 (review remediation; inlined to a single func main per the panic
// corpus convention): cross-frame panic unwind — defers at every frame run
// LIFO during unwind; recover in main observes the original panic value.
// gc: 1237.
func main() {
	order := 0
	var g func(n int)
	g = func(n int) {
		defer func() { order = order*10 + n }()
		if n > 0 {
			g(n - 1)
		} else {
			panic("boom")
		}
	}
	// probe
	func() {
		defer func() {
			if r := recover(); r != nil {
				order = order*10 + 7
			}
		}()
		g(3)
	}()
	_ = order
}

// observe: order
