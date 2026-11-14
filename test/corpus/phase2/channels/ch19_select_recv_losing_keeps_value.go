package main

import "fmt"

// R2313-P1-1 F3 (review CHANGES-REQUESTED): two `=` recv cases, only
// one channel ready. The losing case's target must keep its prior
// value: the copy-back lives inside the case body, so only the
// winning case's copy-back runs. (`.expected` is generated from gc
// 1.27.1 by the fixup-verify runner, never hand-written.)
func main() {
	ch1 := make(chan int, 1)
	ch2 := make(chan int)
	ch1 <- 5
	v := 0
	w := 77
	select {
	case v = <-ch1:
	case w = <-ch2:
	}
	fmt.Println(v, w)
}
