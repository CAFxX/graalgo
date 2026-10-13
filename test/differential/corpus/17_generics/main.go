package main

import "fmt"

type Number interface {
	~int | ~int64 | ~float64 | ~string
}

func Sum[T Number](s []T) T {
	var total T
	for _, v := range s {
		total += v
	}
	return total
}

type Stack[T any] struct{ items []T }

func (s *Stack[T]) Push(v T) { s.items = append(s.items, v) }
func (s *Stack[T]) Pop() T {
	v := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return v
}

func main() {
	fmt.Println(Sum([]int{1, 2, 3}))
	fmt.Println(Sum([]float64{1.5, 2.5}))
	var st Stack[string]
	st.Push("a")
	st.Push("b")
	fmt.Println(st.Pop(), st.Pop())
	m := map[string]int{"b": 2, "a": 1}
	fmt.Println(Sum([]int{m["a"], m["b"]}))
}
