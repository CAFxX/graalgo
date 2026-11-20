// Arrays of arrays (and of structs/interfaces) copied through interfaces,
// pointers, channels, function results and range: boxing must snapshot,
// pointers must alias; == through any; [N]any comparison panics.
package main

type mat [3][2]int

type cell struct {
	v  [2]int8
	ok bool
}

type row [2]cell

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			println(name, "->", r.(interface{ Error() string }).Error())
		}
	}()
	f()
}

//go:noinline
func ident(x any) any { return x }

//go:noinline
func bump(m mat) mat {
	m[1][1]++
	return m
}

func bumpP(m *mat) { m[2][0] += 10 }

func dump(m mat) {
	for _, r := range m {
		for _, x := range r {
			print(x, " ")
		}
	}
	println()
}

func main() {
	var m mat
	m[0] = [2]int{1, 2}
	m[1][0] = 3
	var boxed any = m
	m[0][0] = 100
	dump(boxed.(mat))
	dump(m)

	inner := m[1]
	m[1][1] = 7
	println("inner copy", inner[1], m[1][1])

	p := &m[2]
	q := &p[1]
	*q = 55
	println("ptr alias", m[2][1])
	bumpP(&m)
	dump(m)
	m2 := bump(m)
	println("bump", m[1][1], m2[1][1])

	b2 := ident(m).(mat)
	b2[0][1] = -1
	println("ident copy", m[0][1], b2[0][1])
	println("eq any", boxed == any(mat{{1, 2}, {3, 0}}), ident(m) == any(m), ident(m) == boxed)
	println("eq any differing type", any([2]int{1, 2}) == any(m[0]), any([2]int8{1, 2}) == any([2]int{1, 2}))

	var rows [2]row
	rows[1][0].v[1] = 9
	rows[1][0].ok = true
	cp := rows
	cp[1][0].v[1] = 10
	println("rows copy", rows[1][0].v[1], cp[1][0].v[1], rows == cp)
	cp[1][0].v[1] = 9
	println("rows eq", rows == cp)
	var ra any = rows
	rows[0][1].ok = true
	println("rows boxed", ra.([2]row)[0][1].ok, ra == any(cp))

	for i, r := range rows {
		r[0].ok = !r[0].ok
		rows[i][1].v[0] = int8(i + 1)
		println("range copy", i, r[0].ok, rows[i][0].ok, r[1].v[0])
	}
	for i := range rows {
		pr := &rows[i]
		pr[0].v = [2]int8{int8(i), int8(-i)}
	}
	println("via ptr", rows[1][0].v[1])

	ch := make(chan mat, 1)
	ch <- m
	m[0][0] = 0
	got := <-ch
	println("chan copy", got[0][0], m[0][0])

	ifs := [3]any{1, "x", mat{}}
	ifs2 := ifs
	println("[3]any eq", ifs == ifs2)
	ifs2[2] = mat{{1}}
	println("[3]any ne", ifs == ifs2)
	try("[2]any uncomparable", func() {
		a := [2]any{1, []int{1}}
		b := [2]any{1, []int{1}}
		println(a == b)
	})
	try("[2]any first differs", func() {
		a := [2]any{1, []int{1}}
		b := [2]any{2, []int{1}}
		println("short-circuit", a == b)
	})
	try("struct with any", func() {
		type s struct {
			n int
			v any
		}
		println(s{1, map[int]bool{}} == s{1, map[int]bool{}})
	})
	try("any holding [1]any", func() {
		var x, y any = [1]any{func() {}}, [1]any{func() {}}
		println(x == y)
	})

	arrp := &[4]mat{}
	arrp[3][2][1] = 42
	sl := arrp[2:]
	sl[1][2][1]++
	println("slice of arrays", arrp[3][2][1], len(sl))
	var big [64][8]byte
	for i := range big {
		big[i][i%8] = byte(i)
	}
	bb := any(big)
	big[63][7] = 0
	bc := bb.([64][8]byte)
	println("big boxed", bc[63][7], big[63][7], bc[9][1])
	f := func() [64][8]byte { return big }
	r := f()
	r[0][0] = 1
	println("func ret copy", big[0][0], r[0][0], f() == big)
}
