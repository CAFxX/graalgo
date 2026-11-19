// Targets slice->array and slice->array-pointer conversions (aliasing vs
// copy, length-mismatch panics, nil slice to *[0]T), multi-dimensional
// arrays, and array value semantics on assignment, calls and comparison.
package main

func protect(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(interface{ Error() string }); ok {
				println(name, "panic:", e.Error())
			} else if s, ok := r.(string); ok {
				println(name, "panic str:", s)
			}
		}
	}()
	f()
}

type grid [3][4]int

func fill(g grid) grid {
	for i := range g {
		for j := range g[i] {
			g[i][j] = i*10 + j
		}
	}
	return g
}

func fillPtr(g *grid) {
	for i := range g {
		for j := range g[i] {
			g[i][j] = -(i*10 + j)
		}
	}
}

func sumGrid(g grid) int {
	s := 0
	for _, row := range g {
		for _, v := range row {
			s += v
		}
	}
	return s
}

//go:noinline
func toArr4(s []int) [4]int { return [4]int(s) }

//go:noinline
func toPtr4(s []int) *[4]int { return (*[4]int)(s) }

//go:noinline
func toPtr0(s []int) *[0]int { return (*[0]int)(s) }

//go:noinline
func toArr0(s []int) [0]int { return [0]int(s) }

type pt struct {
	x, y int
	tag  string
}

func main() {
	s := []int{1, 2, 3, 4, 5, 6}
	a := toArr4(s)
	a[0] = 100
	println("arrcopy", s[0], a[0], a[3])
	p := toPtr4(s[2:])
	p[0] = 300
	println("ptralias", s[2], p[0], p[3], len(p))
	protect("arr-short", func() { _ = toArr4(s[:3]) })
	protect("arr-long", func() { a := toArr4(s); println(a[0]) })
	protect("ptr-short", func() { _ = toPtr4(s[4:]) })
	protect("ptr-nil", func() { _ = toPtr4(nil) })
	var ns []int
	p0 := toPtr0(ns)
	println("ptr0nil", p0 == nil)
	p0b := toPtr0(s[:0])
	println("ptr0nonnil", p0b == nil)
	a0 := toArr0(nil)
	println("arr0", len(a0))
	protect("arr0-from-1", func() { _ = toArr0(s[:1]) })
	// byte slice to array of bytes and string
	b := []byte("hello world")
	ba := [5]byte(b)
	b[0] = 'J'
	println("barr", string(ba[:]), string(b[:5]))
	bp := (*[5]byte)(b[6:])
	bp[0] = 'W'
	println("bptr", string(b))
	// multi-dimensional
	var g grid
	g2 := fill(g)
	println("grid", sumGrid(g), sumGrid(g2), g2[2][3])
	fillPtr(&g)
	println("gridptr", sumGrid(g), g[1][2])
	g3 := g2
	g3[0][0] = 999
	println("gridcopy", g2[0][0], g3[0][0], g2 == g3)
	g3[0][0] = 0
	println("grideq", g2 == g3)
	row := g2[1]
	row[0] = 77
	println("row", g2[1][0], row[0])
	rs := g2[1][:]
	rs[0] = 88
	println("rowslice", g2[1][0])
	// array of structs value semantics
	pts := [3]pt{{1, 2, "a"}, {3, 4, "b"}, {5, 6, "c"}}
	pts2 := pts
	pts2[1].tag = "B"
	println("pts", pts[1].tag, pts2[1].tag, pts == pts2)
	for _, q := range pts {
		q.x = 100
	}
	println("rangeval", pts[0].x)
	for i := range pts {
		pts[i].x = 100 + i
	}
	println("rangeidx", pts[0].x, pts[2].x)
	// arrays passed by value to functions
	mod := func(arr [3]pt) [3]pt { arr[0].y = -1; return arr }
	pts3 := mod(pts)
	println("byval", pts[0].y, pts3[0].y)
	// pointer to array indexing and len
	pa := &pts
	pa[2].tag = "C"
	println("ptrarr", pts[2].tag, len(pa), cap(pa[:2]))
	// array of arrays of strings comparisons
	x := [2][2]string{{"a", "b"}, {"c", "d"}}
	y := [2][2]string{{"a", "b"}, {"c", "d"}}
	println("arrcmp", x == y, x != y)
	y[1][1] = "D"
	println("arrcmp2", x == y)
	// arrays with array index expressions
	var cube [2][3][4]byte
	for i := 0; i < 2; i++ {
		for j := 0; j < 3; j++ {
			for k := 0; k < 4; k++ {
				cube[i][j][k] = byte(i*12 + j*4 + k)
			}
		}
	}
	flat := cube[1][2][:]
	println("cube", len(flat), flat[3], cube[0][1][2])
	cc := cube
	cc[1][2][3] = 0
	println("cubecopy", cube[1][2][3], cc[1][2][3])
	// index out of range at runtime on arrays
	protect("arrindex", func() {
		i := 4
		var arr [4]int
		println(arr[i])
	})
	protect("multidim", func() {
		i, j := 1, 5
		println(g[i][j])
	})
	// array conversions of strings
	strs := []string{"p", "q", "r"}
	sa := [3]string(strs)
	strs[0] = "P"
	println("strarr", sa[0], strs[0])
	// zero-length array equality
	var e1, e2 [0]int
	println("zero", e1 == e2, len(e1))
}
