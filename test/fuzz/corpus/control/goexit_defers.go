// Targets runtime.Goexit: it runs all pending defers of the goroutine in
// LIFO order, recover returns nil during Goexit, Goexit from a nested call,
// Goexit inside a deferred func of a recovered panic, and panics recovered
// during a Goexit unwinding not stopping the Goexit.
package main

import (
	"runtime"
	"sort"
	"strings"
	"sync"
)

type log struct {
	mu    sync.Mutex
	lines []string
}

func (l *log) add(s string) {
	l.mu.Lock()
	l.lines = append(l.lines, s)
	l.mu.Unlock()
}

func deep(n int) {
	if n == 0 {
		runtime.Goexit()
	}
	deep(n - 1)
}

func main() {
	var wg sync.WaitGroup
	var l [5]log

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer l[0].add("d1")
		defer func() {
			l[0].add("recover-nil=" + boolStr(recover() == nil))
		}()
		defer l[0].add("d3")
		deep(3)
		l[0].add("unreachable")
	}()
	wg.Wait()

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer l[1].add("outer")
		defer func() {
			if r := recover(); r != nil {
				l[1].add("recovered " + r.(string))
			}
		}()
		defer func() {
			panic("panic during goexit")
		}()
		runtime.Goexit()
	}()
	wg.Wait()

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer l[2].add("after")
		defer func() {
			r := recover()
			l[2].add("got " + r.(string))
			runtime.Goexit()
		}()
		panic("p")
	}()
	wg.Wait()

	done := make(chan string)
	go func() {
		defer func() { done <- "sent-from-defer" }()
		func() {
			defer l[3].add("inner-defer")
			runtime.Goexit()
		}()
		l[3].add("unreachable")
	}()
	l[3].add(<-done)

	results := make(chan int, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { results <- i * i }()
			if i%2 == 0 {
				runtime.Goexit()
			}
		}()
	}
	wg.Wait()
	close(results)
	var got []int
	for v := range results {
		got = append(got, v)
	}
	sort.Ints(got)
	for _, v := range got {
		l[4].add(itoa(v))
	}
	for i := range l {
		println(i, strings.Join(l[i].lines, ","))
	}
	for runtime.NumGoroutine() > 1 {
		runtime.Gosched()
	}
	println("goroutines", runtime.NumGoroutine())
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
