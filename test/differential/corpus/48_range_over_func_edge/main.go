package main

import "fmt"

// 48_range_over_func_edge: range-over-func edge cases (Go 1.23+).
//
// Companion to 43_range_over_func, which covers the happy paths
// (Seq/Seq2/break/continue). This program covers the risk surface of the
// range-over-func lowering: control-flow exits out of the body
// (return/goto/outward labels), body-defer re-homing with LIFO merge
// against enclosing defers, the `=` assignment form, and all four
// iterator-misuse panics with gc's exact messages.
//
// Deterministic by construction (no maps, no concurrency, no wall clock);
// verified byte-identical to gc 1.27.1.

// --- (a) return from a body: sequence defers run before the enclosing
// return completes. ---

func seqReturn(yield func(int) bool) {
	defer fmt.Println("seq defer")
	for i := 0; i < 5; i++ {
		if !yield(i) {
			return
		}
	}
}

func fReturn() int {
	defer fmt.Println("enclosing defer")
	n := 0
	for v := range seqReturn {
		n += v
		if v == 2 {
			return n * 10
		}
	}
	return -1
}

// --- goto out of a body: the sequence's defers still run. ---

func seqGoto(yield func(int) bool) {
	defer fmt.Println("seq defer")
	for i := 0; i < 10; i++ {
		if !yield(i) {
			return
		}
	}
}

func caseGoto() {
	for v := range seqGoto {
		if v == 3 {
			goto done
		}
		fmt.Println(v)
	}
done:
	fmt.Println("done")
}

// --- outward labels: break/continue targeting an outer loop. ---

func seqLabeled(n int) func(func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func caseOutwardLabels() {
outer:
	for a := range seqLabeled(3) {
		for b := range seqLabeled(3) {
			if b == 1 {
				continue outer
			}
			if a == 2 {
				break outer
			}
			fmt.Println(a, b)
		}
	}
	fmt.Println("end")
}

// --- (b) body defers: LIFO among themselves, merged with enclosing defers. ---

func seqBodyDefer(yield func(int) bool) {
	for i := 0; i < 3; i++ {
		if !yield(i) {
			return
		}
	}
}

func caseBodyDeferLIFO() {
	defer fmt.Println("d1")
	for v := range seqBodyDefer {
		fmt.Println("iter", v)
		defer fmt.Println("body", v)
	}
	defer fmt.Println("d2")
	fmt.Println("after loop")
	// gc order: iter 0/1/2, after loop, d2, body 2, body 1, body 0, d1
}

// --- body defer + recover inside the body: iteration continues. ---

func caseBodyDeferRecover() {
	for v := range seqBodyDefer {
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Println("inner recovered:", r)
				}
			}()
			defer fmt.Println("body defer", v)
			panic("boom")
		}()
	}
	fmt.Println("after")
}

// --- body panic: propagates through the sequence's defers, then the
// enclosing defers, in order. ---

func seqBodyPanic(yield func(int) bool) {
	defer fmt.Println("seq defer runs")
	for i := 0; i < 5; i++ {
		if !yield(i) {
			return
		}
	}
}

func caseBodyPanic() {
	defer func() { fmt.Println("recovered:", recover()) }()
	defer fmt.Println("enclosing defer runs")
	for v := range seqBodyPanic {
		fmt.Println("iter", v)
		if v == 2 {
			panic("body boom")
		}
	}
}

// --- nested range-over-func with return from the inner body. ---

func seqNested(n int) func(func(int) bool) {
	return func(yield func(int) bool) {
		defer fmt.Println("inner seq defer", n)
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func fNested() int {
	defer fmt.Println("f defer")
	for a := range seqNested(3) {
		for b := range seqNested(3) {
			if a == 1 && b == 1 {
				return 42
			}
			_ = b
		}
		_ = a
	}
	return -1
}

// --- (d) the `=` assignment form: iteration variable assigned, not declared. ---

func seqAssign(yield func(int) bool) {
	vals := []int{7, 8, 9}
	for _, v := range vals {
		if !yield(v) {
			return
		}
	}
}

func caseAssignForm() {
	var x int
	for x = range seqAssign {
		fmt.Println("x =", x)
	}
	fmt.Println("final x =", x)
}

// --- (c) iterator-misuse panics: gc's exact messages, recovered here so the
// program exits 0 with deterministic output. ---

func caseMisuseYieldAfterFalse() {
	defer func() { fmt.Println("recovered:", recover()) }()
	seq := func(yield func(int) bool) {
		yield(1) // body breaks -> false; sequence misbehaves and yields again
		yield(2)
	}
	for v := range seq {
		fmt.Println("got", v)
		break
	}
}

func caseMisuseYieldAfterExit() {
	defer func() { fmt.Println("recovered:", recover()) }()
	var saved func(int) bool
	seq := func(yield func(int) bool) {
		saved = yield
		yield(1)
	}
	for v := range seq {
		fmt.Println("got", v)
	}
	saved(99) // yield after whole loop exit
}

func caseMisuseSwallowedBodyPanic() {
	defer func() { fmt.Println("recovered:", recover()) }()
	seq := func(yield func(int) bool) {
		defer func() { recover() }() // swallow body panic, return normally
		yield(1)
	}
	for v := range seq {
		fmt.Println("got", v)
		panic("body boom")
	}
}

func caseMisuseReentrantYield() {
	defer func() { fmt.Println("recovered:", recover()) }()
	var saved func(int) bool
	seq := func(yield func(int) bool) {
		saved = yield
		yield(1)
	}
	for v := range seq {
		fmt.Println("got", v)
		saved(2) // re-entrant yield while body is running
	}
}

func main() {
	fmt.Println("--- return-with-defers ---")
	fmt.Println("result:", fReturn())

	fmt.Println("--- goto-out ---")
	caseGoto()

	fmt.Println("--- outward-labels ---")
	caseOutwardLabels()

	fmt.Println("--- body-defer-lifo ---")
	caseBodyDeferLIFO()

	fmt.Println("--- body-defer-recover ---")
	caseBodyDeferRecover()

	fmt.Println("--- body-panic-rethrow ---")
	caseBodyPanic()

	fmt.Println("--- nested-return ---")
	fmt.Println("result:", fNested())

	fmt.Println("--- assign-form ---")
	caseAssignForm()

	fmt.Println("--- misuse-yield-after-false ---")
	caseMisuseYieldAfterFalse()

	fmt.Println("--- misuse-yield-after-exit ---")
	caseMisuseYieldAfterExit()

	fmt.Println("--- misuse-swallowed-body-panic ---")
	caseMisuseSwallowedBodyPanic()

	fmt.Println("--- misuse-reentrant-yield ---")
	caseMisuseReentrantYield()
}
