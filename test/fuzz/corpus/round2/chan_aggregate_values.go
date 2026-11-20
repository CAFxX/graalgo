// Channels carrying structs and arrays by value (unbuffered and buffered),
// receivers blocked when the channel closes (all get zero, ok=false),
// for-range over a channel with break then a later receive, select over
// several ready channels printing only order-independent totals.
package main

import (
	"sort"
	"strconv"
	"sync"
)

type msg struct {
	id   int
	data [3]int16
	tag  string
}

func main() {
	ch := make(chan msg)
	done := make(chan bool)
	go func() {
		m := <-ch
		m.data[0] = -1
		println("recv", m.id, m.data[0], m.data[2], m.tag)
		done <- true
	}()
	out := msg{id: 1, data: [3]int16{7, 8, 9}, tag: "t"}
	ch <- out
	<-done
	println("sender copy intact", out.data[0])

	ach := make(chan [4]float64, 3)
	v := [4]float64{1, 2, 3, 4}
	ach <- v
	v[0] = 10
	ach <- v
	v[0] = 100
	a1, a2 := <-ach, <-ach
	println("array chan", a1[0], a2[0], len(ach), cap(ach))

	blocked := make(chan msg)
	var wg sync.WaitGroup
	results := make([]string, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, ok := <-blocked
			results[i] = strconv.FormatBool(ok) + "/" + strconv.Itoa(m.id) + "/" + m.tag + strconv.Itoa(int(m.data[1]))
		}()
	}
	blocked <- msg{id: 42, tag: "x", data: [3]int16{0, 5}}
	close(blocked)
	wg.Wait()
	sort.Strings(results)
	println("after close", results[0], results[1], results[4])
	m, ok := <-blocked
	println("closed recv", ok, m.id, m.tag == "")

	nums := make(chan int, 10)
	for i := 1; i <= 10; i++ {
		nums <- i
	}
	close(nums)
	sum := 0
	for n := range nums {
		sum += n
		if n == 4 {
			break
		}
	}
	nx, ok := <-nums
	println("range break", sum, nx, ok, len(nums))
	rest := 0
	for n := range nums {
		rest += n
	}
	println("rest", rest)

	c1 := make(chan int, 5)
	c2 := make(chan [2]int, 5)
	c3 := make(chan msg, 5)
	for i := 0; i < 5; i++ {
		c1 <- i
		c2 <- [2]int{i, i * i}
		c3 <- msg{id: i}
	}
	var t1, t2, t3, n1, n2, n3 int
	for k := 0; k < 15; k++ {
		select {
		case x := <-c1:
			t1 += x
			n1++
		case x := <-c2:
			t2 += x[1]
			n2++
		case x := <-c3:
			t3 += x.id
			n3++
		}
	}
	println("select totals", t1, t2, t3, n1+n2+n3)
	select {
	case <-c1:
		println("unexpected")
	default:
		println("all drained", len(c1)+len(c2)+len(c3))
	}

	var nilch chan msg
	select {
	case nilch <- msg{}:
		println("nil send")
	case <-nilch:
		println("nil recv")
	default:
		println("nil chans never ready")
	}

	sc := make(chan struct{}, 2)
	sc <- struct{}{}
	sc <- struct{}{}
	println("empty struct chan", len(sc))
	close(sc)
	cnt := 0
	for range sc {
		cnt++
	}
	println("drained", cnt)

	pc := make(chan *msg, 1)
	pm := &msg{id: 1}
	pc <- pm
	pm.id = 2
	println("ptr chan aliases", (<-pc).id)

	fc := make(chan func() [2]int, 1)
	arr := [2]int{1, 2}
	fc <- func() [2]int { return arr }
	arr[1] = 20
	println("func chan captures", (<-fc)()[1])

	var mu sync.Mutex
	total := 0
	res := make(chan [8]int)
	for w := 0; w < 4; w++ {
		go func() {
			var a [8]int
			for i := range a {
				a[i] = w*8 + i
			}
			res <- a
		}()
	}
	for w := 0; w < 4; w++ {
		a := <-res
		mu.Lock()
		for _, x := range a {
			total += x
		}
		mu.Unlock()
	}
	println("workers total", total)
}
