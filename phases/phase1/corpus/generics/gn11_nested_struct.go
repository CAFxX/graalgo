package main

type Box[T any] struct{ v T }

func main() {
	// 4-deep nested instantiation: the exporter used to collapse
	// Box[Box[Box[Box[int]]]], Box[Box[Box[int]]] and Box[Box[int]] to the
	// same dedup name ("main.Box[main.Box]"), minting a cyclic type graph
	// that overflowed the frontend stack (M10 self-attack P0).
	b4 := Box[Box[Box[Box[int]]]]{Box[Box[Box[int]]]{Box[Box[int]]{Box[int]{42}}}}
	a := b4.v.v.v.v
	// Distinct args at the same nesting depth must not collide either.
	bs := Box[Box[string]]{Box[string]{"hi"}}
	c := len(bs.v.v)
	bi := Box[Box[int]]{Box[int]{7}}
	d := bi.v.v
	_, _, _ = a, c, d
}

// verifies: GG-0014
// observe: a c d
