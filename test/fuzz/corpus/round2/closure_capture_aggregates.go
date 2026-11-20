// Closures capturing arrays and structs by reference vs copying them as
// parameters/locals; per-iteration loop variables of array/struct type;
// closures escaping through slices, maps and interfaces; shared captures
// between sibling closures and captured pointers into captured arrays.
package main

type state struct {
	count int
	buf   [4]int
	name  string
}

func counterPair() (func() int, func() [4]int) {
	var s state
	inc := func() int {
		s.buf[s.count%4] = s.count
		s.count++
		return s.count
	}
	snap := func() [4]int { return s.buf }
	return inc, snap
}

func makeAdders(base [3]int) []func(int) [3]int {
	var fs []func(int) [3]int
	for i := 0; i < 3; i++ {
		fs = append(fs, func(d int) [3]int {
			base[i] += d
			return base
		})
	}
	return fs
}

func main() {
	inc, snap := counterPair()
	inc()
	inc()
	sn := snap()
	inc()
	sn2 := snap()
	println("snap copies", sn[2], sn2[2], inc())

	adders := makeAdders([3]int{10, 20, 30})
	adders[0](1)
	adders[2](3)
	r := adders[1](2)
	println("shared base", r[0], r[1], r[2])

	arr := [3]int{1, 2, 3}
	byRef := func() int { return arr[0] }
	byVal := func(a [3]int) func() int { return func() int { return a[0] } }(arr)
	cp := arr
	byCopy := func() int { return cp[0] }
	arr[0] = 100
	println("ref/val/copy", byRef(), byVal(), byCopy())

	pa := &arr[1]
	mod := func() { *pa *= 2; arr[2]++ }
	mod()
	mod()
	println("ptr into captured", arr[1], arr[2])

	var fns []func() state
	for i, s := range []state{{count: 1}, {count: 2}, {count: 3}} {
		s.buf[0] = i
		s.name = string(rune('a' + i))
		fns = append(fns, func() state { s.count *= 10; return s })
	}
	for _, f := range fns {
		st := f()
		print(st.count, st.buf[0], st.name, " ")
	}
	println()
	println("second call", fns[0]().count)

	for i := 0; i < 3; i++ {
		defer func() { print("defer i=", i, " ") }()
		if i == 2 {
			i++
		}
	}

	m := map[string]func() int{}
	for _, w := range []string{"x", "yy", "zzz"} {
		m[w] = func() int { return len(w) }
	}
	println("map closures", m["x"](), m["yy"](), m["zzz"]())

	var held any
	{
		local := state{name: "boxed"}
		held = func() *state { return &local }
		local.count = 7
	}
	p := held.(func() *state)()
	p.buf[3] = 4
	q := held.(func() *state)()
	println("escaped via any", q.count, q.buf[3], q.name, p == q)

	grid := [2][2]int{}
	setters := []func(int){}
	for i := range grid {
		row := &grid[i]
		for j := range row {
			setters = append(setters, func(v int) { row[j] = v + i*10 + j })
		}
	}
	for k, f := range setters {
		f(k * 100)
	}
	println("grid", grid[0][0], grid[0][1], grid[1][0], grid[1][1])

	acc := [2]float64{}
	add := func(v [2]float64) { acc[0] += v[0]; acc[1] += v[1] }
	v := [2]float64{0.5, 0.25}
	for i := 0; i < 4; i++ {
		add(v)
		v[0] *= 2
	}
	println("acc", acc[0], acc[1])

	var rec func(n int, path [4]int) [4]int
	rec = func(n int, path [4]int) [4]int {
		if n == 4 {
			return path
		}
		path[n] = n * n
		res := rec(n+1, path)
		path[n] = -1
		return res
	}
	pr := rec(0, [4]int{})
	println("recursive array param", pr[0], pr[1], pr[2], pr[3])
	println("done")
}
