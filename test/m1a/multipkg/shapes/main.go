package main

import (
	"example.com/shapes/geom"
	"example.com/shapes/internal/store"
)

var first = geom.NewRect(1, 1, "main-var")

func init() { println("main init", geom.Count, store.Seen) }

func main() {
	store.Shapes.Add(first).Add(geom.NewRect(4, 5, "big"))
	v, ok := geom.Registered("big")
	_, missing := geom.Registered("none")
	println(store.Total(), store.Shapes.Len(), geom.Describe(first), v, ok, missing, geom.Count)
	var s geom.Shape = first
	r := s.(*geom.Rect)
	r.Max = geom.Point{3, 3}
	names := geom.Reduce(&store.Shapes, "", func(acc string, s geom.Shape) string { return acc + geom.Describe(s) + ";" })
	println(s.Area(), names)
}
