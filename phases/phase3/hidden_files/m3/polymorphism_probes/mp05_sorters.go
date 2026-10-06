package main

import "fmt"

type Sorter interface {
	Sort(a []int) []int
	Name() string
}

type Bubble struct{}

func (Bubble) Name() string { return "bubble" }
func (Bubble) Sort(a []int) []int {
	b := append([]int(nil), a...)
	for i := 0; i < len(b); i++ {
		for j := i + 1; j < len(b); j++ {
			if b[j] < b[i] {
				b[i], b[j] = b[j], b[i]
			}
		}
	}
	return b
}

type Inserter struct{}

func (Inserter) Name() string { return "insert" }
func (Inserter) Sort(a []int) []int {
	b := append([]int(nil), a...)
	for i := 1; i < len(b); i++ {
		for j := i; j > 0 && b[j] < b[j-1]; j-- {
			b[j], b[j-1] = b[j-1], b[j]
		}
	}
	return b
}

func main() {
	data := []int{5, 3, 8, 1, 9, 2}
	var s Sorter
	for i := 0; i < 4; i++ {
		if i%2 == 0 {
			s = Bubble{}
		} else {
			s = Inserter{}
		}
		fmt.Println(s.Name(), s.Sort(data))
	}
}
