// Go counterpart to AtomicForm.java: the same six invariants, exercised
// through sync/atomic (backed by the real amd64 LOCK-prefixed assembly).
// Both programs must print RESULT: PASS; the pair is the differential
// evidence that the VarHandle mapping preserves the Go asm's guarantees.
package main

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
)

var failures int

func check(cond bool, name string) {
	if cond {
		fmt.Println("ok   " + name)
	} else {
		fmt.Println("FAIL " + name)
		failures++
	}
}

func main() {
	const T = 8
	const N = 200000

	// T1: AddInt64 atomicity.
	var ctr int64
	var wg sync.WaitGroup
	for t := 0; t < T; t++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < N; i++ {
				atomic.AddInt64(&ctr, 1)
			}
		}()
	}
	wg.Wait()
	check(atomic.LoadInt64(&ctr) == int64(T)*N, "T1 AddInt64: no lost updates")

	// T2: CompareAndSwapInt64 linearizability.
	atomic.StoreInt64(&ctr, 0)
	for t := 0; t < T; t++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50000; i++ {
				for {
					v := atomic.LoadInt64(&ctr)
					if atomic.CompareAndSwapInt64(&ctr, v, v+1) {
						break
					}
				}
			}
		}()
	}
	wg.Wait()
	check(atomic.LoadInt64(&ctr) == int64(T)*50000, "T2 CompareAndSwapInt64: every success installed")

	// T3: no torn 64-bit reads.
	var w64 uint64
	const W = 2000000
	var bad int64
	var wwg sync.WaitGroup
	wwg.Add(1)
	go func() {
		defer wwg.Done()
		for i := uint64(1); i <= W; i++ {
			atomic.StoreUint64(&w64, i)
		}
	}()
	for t := 0; t < 4; t++ {
		wwg.Add(1)
		go func() {
			defer wwg.Done()
			for {
				v := atomic.LoadUint64(&w64)
				if v > W {
					atomic.AddInt64(&bad, 1)
					return
				}
				if v == W {
					return
				}
			}
		}()
	}
	wwg.Wait()
	check(atomic.LoadInt64(&bad) == 0, "T3 LoadUint64/StoreUint64: no torn reads")

	// T4: SwapInt64 returns each predecessor exactly once.
	var s64 int64 = -1
	seen := make([]int64, T)
	for t := 0; t < T; t++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			seen[id] = atomic.SwapInt64(&s64, int64(id))
		}(t)
	}
	wg.Wait()
	m := map[int64]bool{}
	for _, v := range seen {
		m[v] = true
	}
	check(len(m) == T && m[-1], "T4 SwapInt64: each swap observed exactly once")

	// T5: OrInt64 bitwise RMW atomicity, disjoint ranges.
	var b64 int64
	for t := 0; t < T; t++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			bits := int64(0xFF) << (id * 8)
			for i := 0; i < N; i++ {
				atomic.OrInt64(&b64, bits)
			}
		}(t)
	}
	wg.Wait()
	check(atomic.LoadInt64(&b64) == -1, "T5 OrInt64: all bits set exactly")

	// T6: store/load ordering smoke (release/acquire have no direct
	// package-level spelling; use Store/Load which are at least as strong).
	var pay, flg int64
	done := make(chan bool)
	go func() {
		for i := int64(1); i <= 1000000; i++ {
			atomic.StoreInt64(&pay, i)
			atomic.StoreInt64(&flg, i)
		}
		done <- true
	}()
	err := false
	last := int64(0)
	for last < 1000000 {
		f := atomic.LoadInt64(&flg)
		if f != last {
			if atomic.LoadInt64(&pay) < f {
				err = true
			}
			last = f
		}
	}
	<-done
	check(!err, "T6 Store/Load: payload visible with flag")

	if failures == 0 {
		fmt.Println("RESULT: PASS")
	} else {
		fmt.Println("RESULT: FAIL")
		os.Exit(1)
	}
}
