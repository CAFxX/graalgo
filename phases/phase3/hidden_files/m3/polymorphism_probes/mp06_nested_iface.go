package main

import "fmt"

type Doubler interface{ Double() Doubler }
type Valuer interface{ Value() int }

type N struct{ v int }

func (n N) Value() int { return n.v }
func (n N) Double() Doubler { return N{v: n.v * 2} }

func valueOf(d Doubler) int {
	return d.(Valuer).Value()
}

func main() {
	var d Doubler = N{v: 3}
	for i := 0; i < 5; i++ {
		d = d.Double()
	}
	fmt.Println(valueOf(d))
}
