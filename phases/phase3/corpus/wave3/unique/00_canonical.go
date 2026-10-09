package main

import (
	"fmt"
	"unique"
)

func main() {
	// ints: canonicalization + equality + Value
	h1 := unique.Make(42)
	h2 := unique.Make(42)
	h3 := unique.Make(43)
	fmt.Println(h1 == h2) // true
	fmt.Println(h1 == h3) // false
	fmt.Println(h1.Value())

	// strings
	s1 := unique.Make("hello")
	fmt.Println(s1 == unique.Make("hello"))
	fmt.Println(s1 == unique.Make("world"))
	fmt.Println(s1.Value())

	// structs (multi-field, nested)
	type pair struct{ a, b int }
	p1 := unique.Make(pair{1, 2})
	fmt.Println(p1 == unique.Make(pair{1, 2}))
	fmt.Println(p1 == unique.Make(pair{2, 1}))
	v := p1.Value()
	fmt.Println(v.a, v.b)

	// arrays
	a1 := unique.Make([3]int{1, 2, 3})
	fmt.Println(a1 == unique.Make([3]int{1, 2, 3}))
	fmt.Println(a1 == unique.Make([3]int{3, 2, 1}))
	fmt.Println(a1.Value())

	// bools
	fmt.Println(unique.Make(true) == unique.Make(true))
	fmt.Println(unique.Make(true) == unique.Make(false))

	// uint64 boundary values
	fmt.Println(unique.Make(uint64(1)<<63).Value() == uint64(1)<<63)
	fmt.Println(unique.Make(uint64(0)).Value())

	// empty string vs non-empty
	fmt.Println(unique.Make("") == unique.Make(""))
	fmt.Println(unique.Make("") == unique.Make("x"))

	// handles are usable as map keys (comparable)
	m := map[unique.Handle[int]]string{}
	m[unique.Make(7)] = "seven"
	fmt.Println(m[unique.Make(7)])
	fmt.Println(len(m))
}
