package main

import "fmt"

// R2313-P1-1 (blank-ok drive-by): select receive with a blank ok
// target. The blank temp at position 1 must be typed bool, not the
// channel element type, or the builder rejects the ok target.
func main() {
	ch := make(chan int, 1)
	ch <- 4
	select {
	case v, _ := <-ch:
		fmt.Println(v)
	}
}
