package main

// The open-coding limit L = 8 of GG-0071 (test/m1a/defer_path.sh asserts which
// path each function takes): the same defer/panic/recover program at L defer
// statements (open-coded) and at L+1 (the goroutine's defer chain), normal
// exit and panic, with named results modified by the deferred calls. Which
// path a function takes must not be observable.

func atLimit(fail bool) (s string) {
	defer func() { s += "1" }()
	defer func() { s += "2" }()
	defer func() { s += "3" }()
	defer func() { s += "4" }()
	defer func() { s += "5" }()
	defer func() { s += "6" }()
	defer func() {
		if r := recover(); r != nil {
			s += "[" + r.(string) + "]"
		}
	}()
	defer func() { s += "8" }()
	if fail {
		panic("at")
	}
	return "L:"
}

func overLimit(fail bool) (s string) {
	defer func() { s += "1" }()
	defer func() { s += "2" }()
	defer func() { s += "3" }()
	defer func() { s += "4" }()
	defer func() { s += "5" }()
	defer func() { s += "6" }()
	defer func() {
		if r := recover(); r != nil {
			s += "[" + r.(string) + "]"
		}
	}()
	defer func() { s += "8" }()
	defer func() { s += "9" }()
	if fail {
		panic("over")
	}
	return "L+1:"
}

// Under the limit but in a loop: the chain.
func inLoop(n int) (s string) {
	for i := 0; i < n; i++ {
		defer func() { s += string(rune('a' + i)) }()
	}
	return "loop:"
}

// Panic unwinding through alternating open-coded and chained frames runs
// every pending defer exactly once, last in first out.
func outerOpen() {
	defer func() { println("outerOpen recovered:", recover().(string)) }()
	defer println("outerOpen 2")
	middleChain()
}

func middleChain() {
	for i := 0; i < 2; i++ {
		defer println("middleChain", i)
	}
	innerOpen()
}

func innerOpen() {
	defer println("innerOpen")
	panic("through")
}

func main() {
	println(atLimit(false))
	println(atLimit(true))
	println(overLimit(false))
	println(overLimit(true))
	println(inLoop(3))
	outerOpen()
}
