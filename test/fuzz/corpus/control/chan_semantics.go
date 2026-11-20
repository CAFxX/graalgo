// Targets channel semantics: receive from closed returns buffered values
// then zero,false; len/cap of buffered, unbuffered and nil channels; range
// over a closed channel; nil channels never ready in select; select default;
// directional channel conversion; struct and pointer element zero values.
package main

type pt struct {
	x, y int
	s    string
}

func main() {
	c := make(chan int, 4)
	println("len/cap empty", len(c), cap(c))
	c <- 1
	c <- 2
	c <- 3
	println("len/cap 3", len(c), cap(c))
	close(c)
	println("len after close", len(c))
	for i := 0; i < 5; i++ {
		v, ok := <-c
		println("recv", v, ok, len(c))
	}
	var nilc chan int
	println("nil len/cap", len(nilc), cap(nilc))
	u := make(chan string)
	println("unbuf len/cap", len(u), cap(u))

	sc := make(chan pt, 1)
	sc <- pt{1, 2, "a"}
	close(sc)
	p1, ok1 := <-sc
	p2, ok2 := <-sc
	println("struct", p1.x, p1.y, p1.s, ok1, "|", p2.x, p2.y, p2.s == "", ok2)

	pc := make(chan *pt)
	close(pc)
	pp, ok := <-pc
	println("ptr zero", pp == nil, ok)

	r := make(chan int, 10)
	for i := 1; i <= 10; i++ {
		r <- i * i
	}
	close(r)
	sum := 0
	for v := range r {
		sum += v
	}
	println("range sum", sum)

	// select with nil channels and default
	var nr chan int
	var ns chan int
	hits := 0
	for i := 0; i < 100; i++ {
		select {
		case <-nr:
			println("BUG nil recv")
		case ns <- 1:
			println("BUG nil send")
		default:
			hits++
		}
	}
	println("default hits", hits)

	// closed channel always ready, nil never
	cl := make(chan int)
	close(cl)
	closedHits := 0
	for i := 0; i < 50; i++ {
		select {
		case v, ok := <-cl:
			if v == 0 && !ok {
				closedHits++
			}
		case <-nr:
			println("BUG")
		}
	}
	println("closed hits", closedHits)

	// buffered full send goes to default
	full := make(chan int, 1)
	full <- 9
	select {
	case full <- 10:
		println("BUG sent to full")
	default:
		println("full -> default", len(full))
	}

	// directional channels
	bi := make(chan int, 2)
	var so chan<- int = bi
	var ro <-chan int = bi
	so <- 5
	so <- 6
	println("dir", len(ro), cap(so), <-ro, <-ro)

	// select send and receive both possible on one buffered chan: count only
	bc := make(chan int, 1)
	sends, recvs := 0, 0
	for i := 0; i < 20; i++ {
		select {
		case bc <- i:
			sends++
		case <-bc:
			recvs++
		}
	}
	println("alternating", sends, recvs)

	// receive in select with assignment to existing vars
	var x int
	var good bool
	q := make(chan int, 1)
	q <- 77
	select {
	case x, good = <-q:
	}
	println("assign", x, good)
	// channel of channels
	cc := make(chan chan int, 1)
	inner := make(chan int, 1)
	inner <- 3
	cc <- inner
	println("chan of chan", <-<-cc)
}
