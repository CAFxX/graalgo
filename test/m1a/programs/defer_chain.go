package main

// Defers that can run more than once use the goroutine's defer chain.

func loop(n int) (s int) {
	for i := 0; i < n; i++ {
		defer func() { s = s*10 + i }()
	}
	return 0
}

func loopRecover() (out string) {
	for i := 0; i < 3; i++ {
		defer func() {
			if r := recover(); r != nil {
				out += r.(string)
				if i > 0 {
					panic("again")
				}
			}
			out += "."
		}()
	}
	panic("boom")
}

func rangeDefer(xs []string) (out string) {
	for _, x := range xs {
		defer func() { out += x }()
	}
	return
}

func many() (n int) {
	defer func() { n++ }()
	defer func() { n++ }()
	defer func() { n++ }()
	defer func() { n++ }()
	defer func() { n++ }()
	defer func() { n++ }()
	defer func() { n++ }()
	defer func() { n++ }()
	defer func() { n++ }()
	defer func() { n *= 2 }()
	return 1
}

func gotoDefer() (n int) {
	i := 0
again:
	defer func() { n++ }()
	i++
	if i < 4 {
		goto again
	}
	return 100
}

func mixed() (out string) {
	defer func() { out += "T" }()
	for i := 0; i < 2; i++ {
		defer func() { out += "L" }()
	}
	defer func() { out += "B" }()
	return ""
}

func deep(n int) (s int) {
	defer func() {
		if r := recover(); r != nil {
			s = r.(int) + n
			if n > 0 {
				panic(s)
			}
		}
	}()
	if n == 3 {
		panic(1)
	}
	return deep(n + 1)
}

func main() {
	println(loop(4))
	println(loopRecover())
	println(rangeDefer([]string{"a", "b", "c"}))
	println(many())
	println(gotoDefer())
	println(mixed())
	println(deep(0))
}
