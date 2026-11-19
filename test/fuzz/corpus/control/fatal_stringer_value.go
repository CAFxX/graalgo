// Targets the unrecovered panic printout of a value whose type has a
// String() method but no Error() method, wrapped in a [recovered] chain:
// whether gc uses String() for the panic text; exit 2.
package main

import "strconv"

type Point struct{ X, Y int }

func (p Point) String() string {
	return "Point(" + strconv.Itoa(p.X) + "," + strconv.Itoa(p.Y) + ")"
}

func move(p Point, dx int) Point {
	defer func() {
		if r := recover(); r != nil {
			println("move recovered and re-panics with a new value")
			panic(Point{p.X + 100, p.Y})
		}
	}()
	if p.X+dx > 5 {
		panic(Point{p.X + dx, p.Y})
	}
	return Point{p.X + dx, p.Y}
}

func main() {
	p := Point{0, 7}
	for i := 0; i < 5; i++ {
		p = move(p, 2)
		println("at", p.X, p.Y)
	}
}
