package main

import "fmt"

type Acc struct{ total int }

func (a *Acc) Add(n int) int { a.total += n; return a.total }

func main() {
	a := &Acc{}
	f := a.Add // method value binds receiver
	fmt.Println(f(5), f(10))
	g := (*Acc).Add // method expression takes receiver explicitly
	fmt.Println(g(a, 100))
	fmt.Println(a.total)
}
