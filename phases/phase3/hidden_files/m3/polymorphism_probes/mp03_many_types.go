package main

import "fmt"

type IDer interface{ ID() int }

type T1 struct{}

func (T1) ID() int { return 1 }

type T2 struct{}

func (T2) ID() int { return 2 }

type T3 struct{}

func (T3) ID() int { return 3 }

type T4 struct{}

func (T4) ID() int { return 4 }

type T5 struct{}

func (T5) ID() int { return 5 }

type T6 struct{}

func (T6) ID() int { return 6 }

func main() {
	vals := []IDer{T1{}, T2{}, T3{}, T4{}, T5{}, T6{}}
	sum := 0
	for _, v := range vals {
		sum += v.ID()
	}
	fmt.Println(sum)
}
