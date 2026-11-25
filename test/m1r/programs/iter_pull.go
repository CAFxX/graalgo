package main

import (
	"iter"
	"runtime"
	"sync"
)

// iter.Pull and Pull2 on the runtime's coroutines (M3 gap G1: runtime.newcoro,
// runtime.coroswitch): values in order, early stop, stop before the first
// next, yield's result after stop, a panic in the sequence re-raised by next
// (and by stop), runtime.Goexit in the sequence re-raised in the caller, and
// pulls nested inside a sequence.

func count(n int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := range n {
			if !yield(i) {
				println("  seq: yield returned false at", i)
				return
			}
		}
		println("  seq: done")
	}
}

func pairs(s []string) iter.Seq2[int, string] {
	return func(yield func(int, string) bool) {
		for i, v := range s {
			if !yield(i, v) {
				return
			}
		}
	}
}

func catch(what string, f func()) {
	defer func() { println(what, "recovered:", recover().(string)) }()
	f()
}

func main() {
	next, stop := iter.Pull(count(3))
	for {
		v, ok := next()
		if !ok {
			break
		}
		println("next", v)
	}
	v, ok := next()
	println("after end", v, ok)
	stop()

	next, stop = iter.Pull(count(10))
	a, _ := next()
	b, _ := next()
	println("early", a, b)
	stop()
	v, ok = next()
	println("after stop", v, ok)
	stop()

	_, stop = iter.Pull(count(10))
	stop()
	println("stopped before next")

	next2, stop2 := iter.Pull2(pairs([]string{"x", "y", "z"}))
	for {
		k, s, ok := next2()
		if !ok {
			break
		}
		println("pull2", k, s)
	}
	stop2()

	boom := func(yield func(int) bool) {
		yield(1)
		panic("seq boom")
	}
	next, stop = iter.Pull(iter.Seq[int](boom))
	v, _ = next()
	println("before panic", v)
	catch("next", func() { next() })
	v, ok = next()
	println("after panic", v, ok)

	next, stop = iter.Pull(iter.Seq[int](func(yield func(int) bool) {
		yield(7)
		panic("stop boom")
	}))
	next()
	catch("stop", stop)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer println("goexit propagated")
		next, _ := iter.Pull(iter.Seq[int](func(yield func(int) bool) {
			runtime.Goexit()
		}))
		next()
		println("not reached")
	}()
	wg.Wait()

	outer := func(yield func(int) bool) {
		inner, stopInner := iter.Pull(count(3))
		defer stopInner()
		for {
			v, ok := inner()
			if !ok || !yield(v*10) {
				return
			}
		}
	}
	for v := range outer {
		println("nested", v)
	}
	next, stop = iter.Pull(iter.Seq[int](outer))
	v, _ = next()
	println("nested pull", v)
	stop()
}
