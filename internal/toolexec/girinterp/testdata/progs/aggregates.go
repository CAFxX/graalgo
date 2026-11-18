package main

// Array value semantics, struct copy, slices aliasing, append growth.

type P struct {
	X, Y int
	A    [2]int
}

var sink []int

func mod(a [3]int) [3]int { a[0] = 99; return a }
func modp(p *P)           { p.X = 7; p.A[1] = 8 }

func main() {
	a := [3]int{1, 2, 3}
	b := a
	b[0] = 10
	c := mod(a)
	println(a[0], b[0], c[0])
	p := P{X: 1, A: [2]int{5, 6}}
	q := p
	q.A[0] = 50
	modp(&p)
	println(p.X, p.A[0], p.A[1], q.X, q.A[0], q.A[1], p == q)
	ps := []P{p, q}
	ps[0].Y = 3
	println(p.Y, ps[0].Y)
	pp := &ps[1]
	ps[1].X = 42
	println(pp.X)
	s := []int{1, 2, 3, 4, 5}
	t := s[1:3]
	t = append(t, 100)
	println(s[3], len(t), cap(t))
	u := s[1:2:2]
	u = append(u, 200)
	println(s[2], u[1])
	// sink makes the slice escape: gc's stack-buffer append optimization
	// (escape analysis, not the language) would otherwise change caps.
	for i := 0; i < 20; i++ {
		sink = append(sink, i)
		print(cap(sink), " ")
	}
	println()
	var ss []string
	for i := 0; i < 9; i++ {
		ss = append(ss, "x")
	}
	println(cap(ss), cap(append([]byte{}, "hello world!"...)), cap(append([]P(nil), p, p, p)))
	arr := [...]string{2: "c", 0: "a"}
	println(len(arr), arr[0], arr[1] == "", arr[2])
	m := map[[2]int]P{{1, 2}: {X: 1}}
	m[[2]int{1, 2}] = P{X: 2}
	println(len(m), m[[2]int{1, 2}].X)
	n := copy(s, s[2:])
	println(n, s[0], s[1], s[2])
	var nilS []int
	println(nilS == nil, len(nilS), s[:0] == nil)
	ap := &a
	for i, v := range ap {
		ap[2] = 0
		print(i, v, " ")
	}
	println()
	for i, v := range a {
		a[2] = 77
		print(i, v, " ")
	}
	println()
	type pair struct{ k, v int }
	x := struct{ pair }{pair{1, 2}}
	y := x
	y.k = 9
	println(x.k, y.k, x == y)
}
