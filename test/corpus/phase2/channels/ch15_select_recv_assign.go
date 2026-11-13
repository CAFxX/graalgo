package main

import "fmt"

// R2313-P1-1: select receive with an = target whose address is taken
// after the select (boxing is function-wide: the address need not be
// taken inside the case body). The exporter must receive into a plain
// temp and copy back.
func main() {
	ch := make(chan int, 1)
	ch <- 11
	v := 0
	select {
	case v = <-ch:
		fmt.Println(v)
	}
	p := &v
	fmt.Println(*p)
}
