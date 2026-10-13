package main

import "fmt"

type Point struct{ X, Y int }

func (p Point) Add(q Point) Point { return Point{p.X + q.X, p.Y + q.Y} }
func (p *Point) Scale(k int)      { p.X *= k; p.Y *= k }

type Named struct{ Point }

func main() {
	p := Point{1, 2}
	fmt.Println(p.Add(Point{3, 4}))
	p.Scale(10)
	fmt.Println(p)
	n := Named{Point{5, 6}}
	fmt.Println(n.Add(Point{1, 1})) // promoted method
	fmt.Println(n.X, n.Y)           // promoted fields
}
