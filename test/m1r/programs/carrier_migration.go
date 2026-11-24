// carrier_migration: goroutines park and resume — in carrier mode possibly on
// another carrier thread (GG-0073) — while inside calls the runtime made from
// Java (reflect.Value.Call, a time.AfterFunc callback, a sort.Slice less
// function, sync.Once), inside deep recursion and inside deferred calls; then
// they panic, and the panic crosses those calls to a recover. The output is
// gc's; the run also exercises Truffle's per-thread state across a carrier
// switch (the encapsulating-node reference of a call entered on one carrier
// and left on another: make test-thread-mode runs this program with -ea).
package main

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"time"
)

type boom struct{ id, v int }

func (b boom) Error() string { return fmt.Sprintf("boom %d/%d", b.id, b.v) }

// park yields the processor and sleeps: two kinds of park, each a chance to
// resume on another carrier.
func park(n int) {
	for i := 0; i < n; i++ {
		runtime.Gosched()
		time.Sleep(time.Microsecond)
	}
}

func deep(id, d int, ch chan int) int {
	if d == 0 {
		park(2)
		v := <-ch
		park(1)
		if v%3 == 0 {
			panic(boom{id, v})
		}
		return v
	}
	return deep(id, d-1, ch) + 1
}

// viaReflect enters deep through reflect.Value.Call (a Java boundary that
// calls back into Go) and recovers the panic that crosses it, parking inside
// the deferred function first.
func viaReflect(id int, ch chan int) (res int, err error) {
	defer func() {
		park(1)
		if r := recover(); r != nil {
			var b boom
			if e, ok := r.(error); ok && errors.As(e, &b) {
				err = b
			} else {
				err = fmt.Errorf("unexpected %v", r)
			}
		}
	}()
	out := reflect.ValueOf(deep).Call([]reflect.Value{reflect.ValueOf(id), reflect.ValueOf(10), reflect.ValueOf(ch)})
	return int(out[0].Int()), nil
}

func main() {
	const workers, rounds = 32, 4
	chans := make([]chan int, workers)
	for i := range chans {
		chans[i] = make(chan int)
	}
	go func() { // the feeder: one value per worker per round
		for k := 0; k < rounds; k++ {
			for id := 0; id < workers; id++ {
				chans[id] <- id*7 + k
			}
		}
	}()

	var once sync.Once
	shared := 0
	lines := make([]string, workers)
	var wg sync.WaitGroup
	for id := 0; id < workers; id++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			once.Do(func() {
				park(3)
				shared = 42
			})
			sum, panics := 0, 0
			for k := 0; k < rounds; k++ {
				res, err := viaReflect(id, chans[id])
				if err != nil {
					panics++
					sum -= len(err.Error())
				} else {
					sum += res
				}
			}
			xs := []int{id % 5, 9, id % 3, 1, 7, id % 11, 4}
			sort.Slice(xs, func(i, j int) bool {
				if (i+j)%3 == 0 {
					runtime.Gosched()
				}
				return xs[i] < xs[j]
			})
			lines[id] = fmt.Sprintf("worker %d: sum %d panics %d shared %d sorted %v", id, sum, panics, shared, xs)
		}(id)
	}
	wg.Wait()
	for _, l := range lines {
		fmt.Println(l)
	}

	// A timer callback (called by the runtime from Java) that parks, panics
	// and recovers in a deferred call.
	done := make(chan string)
	for i := 0; i < 8; i++ {
		i := i
		time.AfterFunc(time.Millisecond, func() {
			defer func() {
				park(1)
				done <- fmt.Sprint("afterfunc ", i, " recovered ", recover())
			}()
			park(2)
			panic(boom{100 + i, i})
		})
	}
	got := make([]string, 0, 8)
	for i := 0; i < 8; i++ {
		got = append(got, <-done)
	}
	sort.Strings(got)
	for _, g := range got {
		fmt.Println(g)
	}
}
