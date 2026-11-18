package main

// Range over functions and integers; untyped constants in println and
// shifts.

func Count(n int) func(func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				println("stopped at", i)
				return
			}
		}
	}
}

func Pairs(yield func(string, int) bool) {
	_ = yield("a", 1) && yield("b", 2) && yield("c", 3)
}

func find() int {
	for i := range Count(10) {
		if i == 4 {
			return i * 100
		}
	}
	return -1
}

func main() {
	for i := range Count(3) {
		println("i", i)
	}
	for i := range Count(10) {
		if i == 2 {
			break
		}
		println("j", i)
	}
	for k, v := range Pairs {
		if k == "b" {
			continue
		}
		println(k, v)
	}
	println(find())
	var total int64
	for i := range int64(5) {
		total += i
	}
	println(total)
	const c = 1 << 40
	var s uint = 3
	println(c, 'x', 2.5, 1<<s, 1.0, 1e3, 'a'+1)
	var f float32 = 1 << 3
	println(f)
}
