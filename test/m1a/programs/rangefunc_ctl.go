package main

// range-over-func (Go spec, "For statements with range clause"): the loop body
// runs inside the yield function the sequence function calls; break, continue,
// return, goto and labeled jumps leave the body as gc's rewrite does; defer in
// the body attaches to the enclosing function; misuse of yield panics with
// gc's runtime errors.

type pt struct{ x, y int }

func count(n int) func(func(int) bool) {
	return func(yield func(int) bool) {
		defer println("count done", n)
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func pairs(yield func(string, pt) bool) {
	_ = yield("a", pt{1, 2}) && yield("b", pt{3, 4}) && yield("c", pt{5, 6})
}

func times(n int) func(func() bool) {
	return func(yield func() bool) {
		for i := 0; i < n; i++ {
			if !yield() {
				return
			}
		}
	}
}

func basic() {
	for i := range count(3) {
		println("i", i)
	}
	for k, v := range pairs {
		if k == "b" {
			continue
		}
		v.x *= 10 // the iteration value is a copy
		println(k, v.x, v.y)
	}
	n := 0
	for range times(4) {
		n++
	}
	println("times", n)
}

func find(target int) (idx int, found bool) {
	defer func() { idx *= 2 }() // runs after the return inside the body
	for i := range count(10) {
		if i == target {
			return i, true
		}
	}
	return -1, false
}

func labeled() {
outer:
	for i := range count(3) {
		for j := range count(3) {
			if j == 2 {
				continue outer
			}
			if i == 2 {
				break outer
			}
			println("ij", i, j)
		}
	}
	for i := 0; i < 2; i++ {
		for j := range count(5) {
			if j == 1 {
				break
			}
			println("for/range", i, j)
		}
	}
}

func gotoOut() {
	for i := range count(5) {
		if i == 2 {
			goto done
		}
		println("goto loop", i)
	}
	println("unreachable")
done:
	println("after goto")
}

func defers() (s string) {
	defer func() { s += "|first" }()
	for i := range count(3) {
		defer func() { s += "|body" + string(rune('0'+i)) }()
	}
	defer func() { s += "|between" }()
	for _, v := range pairs {
		for j := range count(2) {
			defer func() { s += "|nested" + string(rune('0'+v.x+j)) }()
		}
	}
	return "ret"
}

func closures() {
	var fs []func() int
	for i := range count(3) {
		fs = append(fs, func() int { return i })
	}
	for _, f := range fs {
		println("closure", f())
	}
}

func panicInBody() {
	defer func() { println("recovered body panic:", recover().(string)) }()
	for i := range count(5) {
		if i == 1 {
			panic("from body")
		}
	}
}

var saved func(int) bool

func keep(yield func(int) bool) {
	saved = yield
	yield(0)
}

func misuse() {
	try := func(name string, f func()) {
		defer func() { println(name+":", recover().(error).Error()) }()
		f()
	}
	try("after break", func() {
		for i := range func(yield func(int) bool) {
			yield(1)
			yield(2) // the body already returned false
		} {
			_ = i
			break
		}
	})
	try("after exit", func() {
		for range keep {
		}
		saved(1)
	})
	try("swallowed panic", func() {
		for range func(yield func(int) bool) {
			defer func() { recover() }()
			yield(1)
		} {
			panic("swallowed")
		}
	})
	try("after panic", func() {
		for range func(yield func(int) bool) {
			func() {
				defer func() { recover() }()
				yield(1)
			}()
			yield(2)
		} {
			panic("first")
		}
	})
}

func nilSeq() {
	defer func() { println("nil seq:", recover().(error).Error()) }()
	var f func(func(int) bool)
	for range f {
	}
}

// recover() in a loop body is not called by the deferred function itself.
func recoverInBody() {
	defer func() {
		for range count(1) {
			println("recover in body:", recover() != nil)
		}
		println("recover after loop:", recover() != nil)
	}()
	panic("p")
}

func main() {
	basic()
	println(find(4))
	println(find(20))
	labeled()
	gotoOut()
	println(defers())
	closures()
	panicInBody()
	misuse()
	nilSeq()
	recoverInBody()
}
