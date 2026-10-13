package main

import "fmt"

func main() {
	// Append to nil, multi-append, copy semantics.
	var s []int
	s = append(s, 1, 2, 3)
	fmt.Println(s, len(s), cap(s))
	t := append(s, 4, 5)
	fmt.Println(t, len(t), cap(t))
	// s is unchanged by the append to t only if capacity allowed;
	// print both to pin the aliasing behavior.
	fmt.Println(s, len(s))

	u := make([]int, 2, 4)
	u[0], u[1] = 7, 8
	v := append(u, 9)
	fmt.Println(u, v, len(v), cap(v))

	// Full slice expression.
	w := []int{0, 1, 2, 3, 4}
	fmt.Println(w[1:3], w[:2], w[2:])
	fmt.Println(len(w[1:3]), cap(w[1:3]))
}
