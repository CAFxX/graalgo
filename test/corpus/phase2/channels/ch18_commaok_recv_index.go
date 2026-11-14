package main

import "fmt"

// R2314-P1-1: a comma-ok receive whose value target is an index
// expression (a[i], ok = <-ch) is legal Go and must export. The `:=`
// form with a non-identifier target is NOT legal Go (gc: "non-name
// a[0] on left side of :="), so only the `=` form is exercised here.
func main() {
	ch := make(chan int, 1)
	ch <- 42
	close(ch)
	a := []int{0, 0, 0}
	i := 1
	var ok bool
	a[i], ok = <-ch
	fmt.Println(a[1], ok)
	a[i], ok = <-ch
	fmt.Println(a[1], ok)
}
