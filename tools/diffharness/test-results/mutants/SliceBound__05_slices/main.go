package main

import "fmt"

func main() {
	s := []int{0, 1, 2, 3, 4, 5}
	fmt.Println(s[1 : 4-1])
	t := s[:3]
	fmt.Println(t, len(t), cap(t))
	u := s[2:]
	fmt.Println(u)
	v := make([]int, 0, 4)
	v = append(v, 7, 8, 9)
	fmt.Println(v, len(v), cap(v))
	w := append([]int(nil), s...)
	fmt.Println(w)
	var z []int
	fmt.Println(z == nil, len(z))
}
