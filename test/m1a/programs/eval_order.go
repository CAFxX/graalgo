// Order of evaluation where the Go spec leaves it open and gc decides
// (cmd/compile's order pass): within a statement, calls, receives, appends,
// copies, len/cap, slice expressions, []byte(s) and their like are evaluated
// first, in lexical order; plain operands — variables, fields, element and
// pointer loads, arithmetic, string(b), comparisons — are read afterwards.
// graalgo matches gc (toolexec-design §13, finding 3).
package main

type T struct {
	n  int
	xs []int
}

var g int

func bump(p *int) int {
	*p += 10
	return *p
}

func setg(v int) int {
	g = v
	return v
}

func grow(s *[]int) int {
	*s = append(*s, 99)
	return len(*s)
}

func two(p *int) (int, int) {
	*p *= 2
	return *p, *p + 1
}

func id(v int) int { return v }

func (t *T) inc() int {
	t.n++
	return t.n
}

type I interface{ inc() int }

func main() {
	// Expression lists: a plain variable is read after the calls of the list.
	x := 1
	a, b := x, bump(&x)
	println(a, b)
	x = 1
	c, d, e := x, bump(&x), x
	println(c, d, e)
	x = 1
	println(x, bump(&x), x, bump(&x), x)

	// Operands of an expression.
	x = 1
	println(x+bump(&x), bump(&x)+x, x*2+bump(&x)*0)
	x = 1
	y := x + bump(&x) + x
	println(y)
	x = 1
	y = id(x) + bump(&x)
	println(y)
	g = 1
	println(g, setg(5), g+1, setg(7), -g)

	// Call arguments: an argument without a call is read when the call is made.
	x = 1
	println(id(x) + id(bump(&x)))
	x = 1
	f3 := func(p, q, r int) int { return p*100 + q*10 + r }
	println(f3(x, bump(&x), x), f3(bump(&x), x, bump(&x)))

	// Fields, elements, pointer loads are plain operands too.
	t := &T{n: 1, xs: []int{1, 2, 3}}
	println(t.n, t.inc(), t.n, t.xs[0], grow(&t.xs), len(t.xs))
	arr := [3]int{1, 2, 3}
	p := &arr[0]
	println(*p, bump(p), arr[0], bump(&arr[0]), *p+arr[0])
	var i I = t
	println(t.n, i.inc(), t.n)

	// len, cap, slice expressions and conversions to []byte are evaluated in
	// order with the calls; string(b) is a plain operand.
	s := []int{1, 2, 3}
	println(len(s), grow(&s), len(s), cap(s) >= 4)
	s = []int{1, 2, 3}
	u := s[:2]
	println(len(s[1:]), grow(&s), len(s[1:]), len(u))
	bs := []byte("abc")
	mut := func() int { bs[0] = 'X'; return 1 }
	println(string(bs), mut(), string(bs))
	str := "abc"
	chg := func() int { str = "xyz!"; return 1 }
	println(len([]byte(str)), chg(), len([]byte(str)), len(str))
	dst := make([]byte, 5)
	println(copy(dst, "hello"), string(dst), copy(dst[:2], "xy"), string(dst))

	// Assignment statements.
	x = 1
	z := 0
	z = x + bump(&x)
	println(z)
	x = 1
	x += bump(&x)
	println(x)
	x = 1
	q, r := 0, 0
	q, r = x, bump(&x)
	println(q, r)
	x = 3
	m, n := two(&x)
	println(m, n, x)
	vals := make([]int, 12)
	k := 0
	vals[k], k = bump(&k), 2
	println(vals[0], vals[10], k)
	s = []int{1, 2, 3}
	s[0] = grow(&s)
	println(s[0], len(s))
	tt := T{n: 1}
	pt := &tt
	other := T{n: 100}
	swap := func() int { pt = &other; return 5 }
	pt.n = swap()
	println(tt.n, other.n)

	// Returns, sends, conditions, composite literals, short-circuit operands.
	ret := func() (int, int) { v := 1; return v, bump(&v) }
	println(ret())
	ch := make(chan int, 4)
	x = 1
	ch <- x + bump(&x)
	println(<-ch)
	x = 1
	if x < bump(&x) {
		println("lt")
	} else {
		println("ge")
	}
	x = 1
	lit := []int{x, bump(&x), x}
	st := T{n: x, xs: []int{bump(&x), x}}
	println(lit[0], lit[1], lit[2], st.n, st.xs[0], st.xs[1])
	x = 1
	ok := x > 5 && bump(&x) > 0
	println(ok, x)
	ok = x == 1 || bump(&x) > 0
	println(ok, x)
	println(x, x == 1 && bump(&x) > 0, x)
	x = 1
	for j := 0; j < bump(&x) && j < 3; j++ {
		print(j, x, " ")
	}
	println(x)
	x = 1
	switch x {
	case bump(&x) - 10:
		println("first", x)
	default:
		println("default", x)
	}

	// Receives and type assertions.
	ch <- 1
	ch <- 2
	x = 1
	println(x, <-ch+bump(&x), x, <-ch)
	var e2 any = x
	println(e2.(int), bump(&x), e2.(int), x)
	mp := map[string]int{"a": 1}
	chm := func() int { mp["a"] = 50; return 2 }
	println(mp["a"], chm(), mp["a"])
}
