package main

import "fmt"

// R2313-P1-1: comma-ok receive statement into a target whose address
// is taken afterwards. The statement desugars to a single-case select,
// so the same temp + copy-back rule applies as for select cases.
func main() {
	ch := make(chan int, 1)
	ch <- 3
	v, ok := <-ch
	p := &v
	fmt.Println(*p, ok)
}
