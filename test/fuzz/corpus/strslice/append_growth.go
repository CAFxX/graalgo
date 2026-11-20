// Targets gc's exact append growth formula (growslice + size-class rounding)
// for element sizes 1,2,4,8,12,16,24,32,40 bytes, single-element appends,
// bulk appends, appends from make with odd caps, and []byte(string) caps.
package main

var sink []any

type e12 struct{ a, b, c int32 }
type e24 struct{ a, b, c int64 }
type e40 struct{ a, b, c, d, e int64 }

func trace1(name string, n int) {
	var s []byte
	last := -1
	print(name)
	for i := 0; i < n; i++ {
		s = append(s, byte(i))
		if cap(s) != last {
			last = cap(s)
			print(" ", len(s), ":", last)
		}
	}
	sink = append(sink, s)
	println()
}

func trace2(n int) {
	var s []int16
	last := -1
	print("int16")
	for i := 0; i < n; i++ {
		s = append(s, int16(i))
		if cap(s) != last {
			last = cap(s)
			print(" ", len(s), ":", last)
		}
	}
	sink = append(sink, s)
	println()
}

func trace4(n int) {
	var s []int32
	last := -1
	print("int32")
	for i := 0; i < n; i++ {
		s = append(s, int32(i))
		if cap(s) != last {
			last = cap(s)
			print(" ", len(s), ":", last)
		}
	}
	sink = append(sink, s)
	println()
}

func trace8(n int) {
	var s []int64
	last := -1
	print("int64")
	for i := 0; i < n; i++ {
		s = append(s, int64(i))
		if cap(s) != last {
			last = cap(s)
			print(" ", len(s), ":", last)
		}
	}
	sink = append(sink, s)
	println()
}

func trace12(n int) {
	var s []e12
	last := -1
	print("e12")
	for i := 0; i < n; i++ {
		s = append(s, e12{a: int32(i)})
		if cap(s) != last {
			last = cap(s)
			print(" ", len(s), ":", last)
		}
	}
	sink = append(sink, s)
	println()
}

func trace16(n int) {
	var s []string
	last := -1
	print("string")
	for i := 0; i < n; i++ {
		s = append(s, "x")
		if cap(s) != last {
			last = cap(s)
			print(" ", len(s), ":", last)
		}
	}
	sink = append(sink, s)
	println()
}

func trace24(n int) {
	var s []e24
	last := -1
	print("e24")
	for i := 0; i < n; i++ {
		s = append(s, e24{a: int64(i)})
		if cap(s) != last {
			last = cap(s)
			print(" ", len(s), ":", last)
		}
	}
	sink = append(sink, s)
	println()
}

func trace32(n int) {
	var s [][4]int64
	last := -1
	print("a32")
	for i := 0; i < n; i++ {
		s = append(s, [4]int64{int64(i)})
		if cap(s) != last {
			last = cap(s)
			print(" ", len(s), ":", last)
		}
	}
	sink = append(sink, s)
	println()
}

func trace40(n int) {
	var s []e40
	last := -1
	print("e40")
	for i := 0; i < n; i++ {
		s = append(s, e40{a: int64(i)})
		if cap(s) != last {
			last = cap(s)
			print(" ", len(s), ":", last)
		}
	}
	sink = append(sink, s)
	println()
}

//go:noinline
func bulkBytes(start, add int) (int, int) {
	s := make([]byte, start)
	s = append(s, make([]byte, add)...)
	sink = append(sink, s)
	return len(s), cap(s)
}

//go:noinline
func bulkInt64(start, add int) (int, int) {
	s := make([]int64, start)
	s = append(s, make([]int64, add)...)
	sink = append(sink, s)
	return len(s), cap(s)
}

//go:noinline
func bulkE24(start, add int) (int, int) {
	s := make([]e24, start)
	s = append(s, make([]e24, add)...)
	sink = append(sink, s)
	return len(s), cap(s)
}

//go:noinline
func bulkE12(start, add int) (int, int) {
	s := make([]e12, start)
	s = append(s, make([]e12, add)...)
	sink = append(sink, s)
	return len(s), cap(s)
}

func main() {
	trace1("byte", 5000)
	trace2(3000)
	trace4(3000)
	trace8(3000)
	trace12(2000)
	trace16(2000)
	trace24(2000)
	trace32(1500)
	trace40(1500)
	starts := []int{0, 1, 3, 5, 7, 9, 17, 100, 255, 256, 257, 511, 1000}
	adds := []int{1, 2, 7, 33, 100, 513, 2000}
	for _, st := range starts {
		for _, ad := range adds {
			l1, c1 := bulkBytes(st, ad)
			l2, c2 := bulkInt64(st, ad)
			l3, c3 := bulkE24(st, ad)
			l4, c4 := bulkE12(st, ad)
			println("bulk", st, ad, l1, c1, l2, c2, l3, c3, l4, c4)
		}
	}
	// append to slice with spare capacity does not reallocate
	s := make([]int, 3, 10)
	t := append(s, 1, 2)
	t[0] = 99
	sink = append(sink, s, t)
	println("spare", s[0], len(t), cap(t))
	// append beyond cap reallocates
	u := append(s[:3:3], 7)
	u[1] = 55
	sink = append(sink, u)
	println("realloc", s[1], len(u), cap(u))
	// []byte(string) and append(string...)
	for _, n := range []int{0, 1, 5, 31, 32, 33, 100} {
		str := string(make([]byte, n))
		b := []byte(str)
		b2 := append([]byte(nil), str...)
		b3 := append([]byte("ab"), str...)
		sink = append(sink, b, b2, b3)
		println("strbytes", n, len(b), len(b2), cap(b2), len(b3), cap(b3))
	}
	// append of zero elements to nil stays nil
	var z []int
	z = append(z)
	println("nilappend", z == nil, len(z), cap(z))
	z = append(z, []int{}...)
	println("nilappend2", z == nil)
	// growth from odd caps
	for _, c := range []int{1, 3, 6, 11, 13, 27} {
		o := make([]int32, c, c)
		o = append(o, 1)
		p := make([]e40, c, c)
		p = append(p, e40{})
		sink = append(sink, o, p)
		println("odd", c, cap(o), cap(p))
	}
}
