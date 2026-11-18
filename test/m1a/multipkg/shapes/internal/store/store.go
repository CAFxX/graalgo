// Package store depends on geom: it must initialize after it.
package store

import "example.com/shapes/geom"

var Shapes geom.List[geom.Shape]

var Seen = geom.Count

func init() {
	Shapes.Add(geom.NewRect(2, 3, "store"))
}

func Total() int {
	return geom.Reduce(&Shapes, 0, func(acc int, s geom.Shape) int { return acc + s.Area() })
}
