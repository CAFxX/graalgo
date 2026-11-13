package main

import "fmt"

// R2313-P1-1: select receive into a target whose address is taken in
// the case body. The exporter must receive into a plain temp and copy
// back; receiving into the boxed target fails the build.
func main() {
	ch := make(chan int, 1)
	ch <- 42
	select {
	case v := <-ch:
		p := &v
		fmt.Println(*p)
	}
}
