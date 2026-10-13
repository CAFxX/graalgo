package main

import "fmt"

func main() {
	x := 42
	p := &x
	fmt.Println(*p)
	*p = 100
	fmt.Println(x)
	pp := &p
	fmt.Println(**pp)
	fmt.Println(p == &x)
	var q *int
	fmt.Println(q == nil)
	arr := [3]int{1, 2, 3}
	fmt.Println((*[3]int)(&arr)[2])
}
