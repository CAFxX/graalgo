// Targets goroutine pipelines: generator -> squarer stages -> fan-in merge
// with WaitGroup closing the output, unbuffered handoff, worker pools with
// results collected and sorted, and a ping-pong goroutine pair with strict
// alternation through unbuffered channels.
package main

import (
	"slices"
	"strconv"
	"strings"
	"sync"
)

func gen(n int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for i := 1; i <= n; i++ {
			out <- i
		}
	}()
	return out
}

func sq(in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for v := range in {
			out <- v * v
		}
	}()
	return out
}

func merge(cs ...<-chan int) <-chan int {
	var wg sync.WaitGroup
	out := make(chan int, 3)
	for _, c := range cs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for v := range c {
				out <- v
			}
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

func pingPong(n int) string {
	ping := make(chan int)
	pong := make(chan int)
	var b strings.Builder
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		for v := range ping {
			mu.Lock()
			b.WriteString("pi" + strconv.Itoa(v) + " ")
			mu.Unlock()
			pong <- v + 1
		}
		close(pong)
	}()
	go func() {
		defer close(done)
		v := 0
		for v < n {
			ping <- v
			v = <-pong
			mu.Lock()
			b.WriteString("po" + strconv.Itoa(v) + " ")
			mu.Unlock()
		}
		close(ping)
		for range pong {
		}
	}()
	<-done
	return b.String()
}

func workerPool(jobs []string, workers int) []string {
	in := make(chan string)
	out := make(chan string)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range in {
				out <- strings.ToUpper(j) + ":" + strconv.Itoa(len(j))
			}
		}()
	}
	go func() {
		for _, j := range jobs {
			in <- j
		}
		close(in)
	}()
	go func() {
		wg.Wait()
		close(out)
	}()
	var res []string
	for r := range out {
		res = append(res, r)
	}
	slices.Sort(res)
	return res
}

func main() {
	src := gen(30)
	total := 0
	count := 0
	var all []int
	for v := range merge(sq(src), sq(src), sq(src)) {
		total += v
		count++
		all = append(all, v)
	}
	slices.Sort(all)
	println("pipeline count", count, "total", total, "first", all[0], "last", all[len(all)-1])
	println(pingPong(6))
	for _, r := range workerPool([]string{"go", "graal", "truffle", "defer", "select", "x"}, 3) {
		println(r)
	}
	// unbuffered handoff: sender blocks until receiver ready; order preserved per channel
	c := make(chan int)
	go func() {
		for i := 0; i < 5; i++ {
			c <- i
		}
		close(c)
	}()
	prev := -1
	inOrder := true
	for v := range c {
		if v != prev+1 {
			inOrder = false
		}
		prev = v
	}
	println("in order", inOrder, prev)
}
