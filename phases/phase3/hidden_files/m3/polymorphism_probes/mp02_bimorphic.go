package main

import "fmt"

type Sizer interface{ Size() int }

type A struct{ n int }

func (a A) Size() int { return a.n }

type B struct{ m int }

func (b B) Size() int { return b.m * 2 }

func main() {
	var s Sizer
	total := 0
	for i := 0; i < 100; i++ {
		if i%2 == 0 {
			s = A{n: i}
		} else {
			s = B{m: i}
		}
		total += s.Size()
	}
	fmt.Println(total)
}
