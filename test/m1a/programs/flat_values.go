package main

// The flat value model: nested structs and arrays, value copies, interior pointers.

type Point struct{ X, Y int }

type Rect struct {
	Min, Max Point
	Name     string
	Tags     [3]uint8
}

type Grid struct {
	Cells [2][3]Point
	Rects []Rect
}

func move(p Point, d int) Point { p.X += d; return p }

func (r *Rect) grow(d int) { r.Max.X += d; r.Max.Y += d }

func (r Rect) area() int { return (r.Max.X - r.Min.X) * (r.Max.Y - r.Min.Y) }

func main() {
	r := Rect{Min: Point{1, 2}, Max: Point{4, 6}, Name: "r", Tags: [3]uint8{1, 2, 255}}
	s := r
	s.Min.X = 100
	s.Tags[2]++
	println(r.Min.X, s.Min.X, r.Tags[2], s.Tags[2], r.area())

	p := &r.Max
	p.X = 40
	q := &r.Max.Y
	*q += 10
	println(r.Max.X, r.Max.Y, p == &r.Max, q == &r.Max.Y, p.Y)
	r.grow(1)
	println(r.Max.X, r.Max.Y, *q)

	var g Grid
	g.Cells[1][2] = Point{7, 8}
	row := g.Cells[1]
	row[2].X = 70
	cell := &g.Cells[1][2]
	cell.Y++
	println(g.Cells[1][2].X, g.Cells[1][2].Y, row[2].X, len(g.Cells), len(g.Cells[0]))

	g.Rects = append(g.Rects, r, s)
	g.Rects[0].Min = move(g.Rects[0].Min, 5)
	rp := &g.Rects[1]
	rp.Name = "second"
	rp.Tags[0] = 9
	println(g.Rects[0].Min.X, r.Min.X, g.Rects[1].Name, s.Name, g.Rects[1].Tags[0], s.Tags[0])

	// A slice of an array field shares the struct's storage.
	tags := r.Tags[:]
	tags[1] = 77
	sub := g.Cells[0][1:]
	sub[0].X = 5
	println(r.Tags[1], len(tags), cap(tags), g.Cells[0][1].X, len(sub), cap(sub))

	arr := [4]int{1, 2, 3, 4}
	brr := arr
	brr[0] = 10
	sl := arr[1:3:4]
	sl = append(sl, 9)
	sl = append(sl, 8)
	sl[0] = 20
	println(arr[0], brr[0], arr[1], arr[3], sl[0], len(sl), arr == [4]int{1, 2, 3, 9}, r == s)

	type pair struct {
		a  any
		f  float32
		ok bool
	}
	x := pair{a: Point{1, 2}, f: 1.5, ok: true}
	y := x
	y.f *= 2
	println(x == y, x.a == y.a, x.f, y.f, x.a.(Point).Y)

	ptrs := map[*Point]string{}
	a, b := &g.Cells[0][0], &g.Cells[0][0]
	ptrs[a] = "first"
	ptrs[b] = "same"
	ptrs[&g.Cells[0][1]] = "other"
	println(len(ptrs), ptrs[a])

	m := map[Point]Rect{}
	m[Point{1, 1}] = r
	v := m[Point{1, 1}]
	v.Name = "copy"
	println(m[Point{1, 1}].Name, v.Name, m[Point{2, 2}].Name == "", len(m))

	np := new(Point)
	np.X = 3
	ni := new(int)
	*ni = 5
	pp := &np
	(*pp).Y = 4
	println(np.X+np.Y, *ni, *pp == np)
}
