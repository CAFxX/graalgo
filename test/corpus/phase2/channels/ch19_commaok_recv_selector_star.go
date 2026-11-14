package main

import "fmt"

// R2314-P1-1: comma-ok receives with selector and dereference value
// targets (s.f, ok = <-ch; *p, ok = <-ch) are legal Go and must export.
// The `:=` form with a non-identifier target is NOT legal Go (gc:
// "non-name t.f on left side of :="), so only the `=` form is
// exercised here.
type S struct{ f int }

func main() {
	ch := make(chan int, 1)
	ch <- 5
	close(ch)
	s := S{}
	var ok bool
	s.f, ok = <-ch
	fmt.Println(s.f, ok)
	s.f, ok = <-ch
	fmt.Println(s.f, ok)
	ch2 := make(chan int, 1)
	ch2 <- 3
	close(ch2)
	x := 0
	p := &x
	*p, ok = <-ch2
	fmt.Println(x, ok)
	*p, ok = <-ch2
	fmt.Println(x, ok)
}
