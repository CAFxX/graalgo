// Regression for review T2 (2026-11-02): an unlabeled `break` in a select
// case body must exit the select, not the enclosing loop, and must not
// crash at function top level. Covers break in a recv case, a send case,
// and a default case, inside a loop and at top level, plus a labeled break
// to the select itself, a labeled break through the select to an outer
// loop (which must still exit the loop), and `continue` through a select
// case (which must still continue the loop). All channels are buffered
// (or never ready, for the default cases), so the output is deterministic
// and independent of scheduling.
package main

import "fmt"

func main() {
	// 1: recv-case break inside a loop: loop must continue, post-select must run.
	ch := make(chan int, 1)
	for i := 0; i < 3; i++ {
		ch <- i
		select {
		case v := <-ch:
			fmt.Println("recv", v)
			break
		}
		fmt.Println("after-recv", i)
	}
	// 2: send-case break inside a loop.
	out := make(chan int, 1)
	for i := 0; i < 3; i++ {
		select {
		case out <- i:
			break
		}
		fmt.Println("after-send", <-out)
	}
	// 3: default-case break inside a loop.
	empty := make(chan int)
	for i := 0; i < 3; i++ {
		select {
		case <-empty:
			fmt.Println("unexpected")
		default:
			break
		}
		fmt.Println("after-default", i)
	}
	// 4: recv-case break at top level (no loop).
	top := make(chan int, 1)
	top <- 42
	select {
	case v := <-top:
		fmt.Println("top-recv", v)
		break
	}
	fmt.Println("after-top-recv")
	// 5: send-case break at top level.
	topSend := make(chan int, 1)
	select {
	case topSend <- 7:
		break
	}
	fmt.Println("after-top-send", <-topSend)
	// 6: default-case break at top level.
	select {
	case <-empty:
		fmt.Println("unexpected2")
	default:
		break
	}
	fmt.Println("after-top-default")
	// 7: labeled break to the select itself, inside a loop.
	for i := 0; i < 2; i++ {
	Sel:
		select {
		case v := <-ch:
			fmt.Println("labeled-unexpected", v)
		default:
			break Sel
		}
		fmt.Println("after-labeled-select", i)
		ch <- 99
		_ = <-ch
	}
	// 8: labeled break through the select to an outer loop.
Outer:
	for i := 0; i < 5; i++ {
		ch <- i
		select {
		case <-ch:
			break Outer
		}
		fmt.Println("after-through-never", i)
	}
	fmt.Println("after-outer-break")
	// 9: continue through a select case to the loop.
	for i := 0; i < 3; i++ {
		ch <- i
		select {
		case <-ch:
			if i < 2 {
				continue
			}
			fmt.Println("continue-last", i)
		}
		fmt.Println("after-continue-never", i)
	}
	fmt.Println("done")
}
