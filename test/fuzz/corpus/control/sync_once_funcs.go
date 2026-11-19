// Targets sync.Once (runs exactly once across goroutines, a panicking Do is
// considered done), and sync.OnceFunc/OnceValue/OnceValues re-panicking the
// same value on every call after the first panic, plus their caching.
package main

import (
	"errors"
	"strconv"
	"sync"
)

func catch(f func()) (s string) {
	defer func() {
		switch v := recover().(type) {
		case nil:
			s = "no panic"
		case string:
			s = "panic string " + v
		case error:
			s = "panic error " + v.Error()
		case int:
			s = "panic int " + strconv.Itoa(v)
		default:
			s = "panic other"
		}
	}()
	f()
	return
}

func main() {
	var once sync.Once
	calls := 0
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Go(func() { once.Do(func() { calls++ }) })
	}
	wg.Wait()
	println("once calls", calls)

	var po sync.Once
	println(catch(func() { po.Do(func() { panic("once-boom") }) }))
	ran := false
	println(catch(func() { po.Do(func() { ran = true }) }))
	println("second Do ran", ran)

	// Once.Do re-entrancy from another Once is fine
	var o1, o2 sync.Once
	order := ""
	o1.Do(func() {
		order += "o1"
		o2.Do(func() { order += "o2" })
	})
	o2.Do(func() { order += "BAD" })
	println("nested once", order)

	n := 0
	f := sync.OnceFunc(func() { n++ })
	f()
	f()
	f()
	println("OnceFunc n", n)

	pcount := 0
	pf := sync.OnceFunc(func() {
		pcount++
		panic(errors.New("oncefunc-err"))
	})
	println(catch(pf))
	println(catch(pf))
	println(catch(pf))
	println("OnceFunc panic count", pcount)

	vcount := 0
	v := sync.OnceValue(func() int {
		vcount++
		return 42 + vcount
	})
	println("OnceValue", v(), v(), vcount)

	pv := sync.OnceValue(func() string {
		panic(7)
	})
	println(catch(func() { _ = pv() }))
	println(catch(func() { _ = pv() }))

	vs := sync.OnceValues(func() (int, error) {
		return 3, errors.New("vs-err")
	})
	a, err := vs()
	b, err2 := vs()
	println("OnceValues", a, b, err.Error(), err == err2)

	pvs := sync.OnceValues(func() (string, bool) {
		panic("values-boom")
	})
	println(catch(func() { pvs() }))
	println(catch(func() { pvs() }))

	// concurrent OnceValue sees one computation
	cnt := 0
	cv := sync.OnceValue(func() int { cnt++; return 99 })
	sum := 0
	var mu sync.Mutex
	for i := 0; i < 10; i++ {
		wg.Go(func() {
			x := cv()
			mu.Lock()
			sum += x
			mu.Unlock()
		})
	}
	wg.Wait()
	println("concurrent OnceValue", sum, cnt)

	// panic(nil) inside OnceFunc: every call re-panics with PanicNilError.
	gf := sync.OnceFunc(func() { panic(nil) })
	println(catch(gf))
	println(catch(gf))
}
