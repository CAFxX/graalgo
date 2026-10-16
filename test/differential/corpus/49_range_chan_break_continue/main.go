package main

// FE-4 / FE-18: break/continue binding in `for v := range ch` bodies.
// The range-over-chan desugar (ADR-0005 App C) copies the body inside a
// synthesized select: labeled continue must target the loop (FE-4), and
// unlabeled break must exit the loop rather than reselect (FE-18).

import "fmt"

func main() {
	ch := make(chan int, 6)
	for i := 1; i <= 6; i++ {
		ch <- i
	}
	close(ch)

	// FE-4: labeled continue on range-over-chan (pre-fix: uncaught
	// ContinueSignal at runtime).
	sum := 0
outer:
	for v := range ch {
		if v%2 == 0 {
			continue outer
		}
		sum += v
	}
	fmt.Println("sum", sum) // 1+3+5 = 9

	// FE-18: unlabeled break exits the loop.
	ch2 := make(chan int, 4)
	ch2 <- 10
	ch2 <- 20
	ch2 <- 30
	close(ch2)
	n := 0
	for v := range ch2 {
		n++
		if v == 20 {
			break
		}
	}
	fmt.Println("n", n) // 2

	// A source-level select nested in the body still works: its cases run
	// per iteration and the loop terminates on close.
	ch3 := make(chan int, 2)
	ch3 <- 7
	close(ch3)
	m := 0
	for v := range ch3 {
		m++
		select {
		case x := <-ch3:
			_ = x
		default:
		}
		_ = v
	}
	fmt.Println("m", m) // 1

	// Labeled break out of the range loop from a nested if.
	ch4 := make(chan int, 3)
	ch4 <- 1
	ch4 <- 2
	ch4 <- 3
	close(ch4)
	k := 0
done:
	for v := range ch4 {
		k++
		if v == 2 {
			break done
		}
	}
	fmt.Println("k", k) // 2
}
