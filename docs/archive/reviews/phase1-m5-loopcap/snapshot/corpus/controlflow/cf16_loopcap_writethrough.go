package main

// Loop-capture: body writes to the for-header variable write through to the
// outer cell, so cond/post observe them (gc-oracle: post sees the body write).
func main() {
	count := 0
	for i := 0; i < 5; i++ {
		if i == 2 {
			i = 10
		}
		count++
	}
	_ = count
}

// observe: count
