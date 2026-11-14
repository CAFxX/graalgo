package main

import "fmt"

// R2314-P1-1: the ok position of a comma-ok receive may also be a
// non-identifier target (v, okArr[0] = <-ch; both positions
// non-identifier), a global identifier target desugars through a temp
// (g, ok = <-ch), and a blank ok (w, _ = <-ch) takes a bool temp.
var g int

func main() {
	ch1 := make(chan int, 1)
	ch1 <- 6
	close(ch1)
	v := 0
	okArr := [1]bool{}
	var ok bool
	v, okArr[0] = <-ch1
	fmt.Println(v, okArr[0])
	v, okArr[0] = <-ch1
	fmt.Println(v, okArr[0])
	ch2 := make(chan int, 1)
	ch2 <- 7
	close(ch2)
	buf := [1]int{}
	okArr2 := [1]bool{}
	buf[0], okArr2[0] = <-ch2
	fmt.Println(buf[0], okArr2[0])
	ch3 := make(chan int, 1)
	ch3 <- 9
	close(ch3)
	g, ok = <-ch3
	fmt.Println(g, ok)
	ch4 := make(chan int, 1)
	ch4 <- 8
	close(ch4)
	w := 0
	w, _ = <-ch4
	fmt.Println(w)
}
