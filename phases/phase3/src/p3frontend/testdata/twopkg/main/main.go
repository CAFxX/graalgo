package main

import (
	"example.com/twopkg/helper"
	"fmt"
)

// T collides with helper.T by name (P0-1): after flattening, helper's
// embedded T must still denote helper's type, not this one.
type T struct {
	X int
}

// Who identifies main's T.
func (t T) Who() string { return "main.T" }

// Counter shadows helper.Counter at package level (methodology red-green
// P1-2): a Uses-name-based rename mutant merges the two QUIETLY — the
// differential test must catch it.
var Counter = 1000

func main() {
	fmt.Println(helper.Greeting)
	fmt.Println(helper.Add(40, 2))
	p := helper.Point{X: 1, Y: 2}
	fmt.Println(p.String())
	fmt.Println(helper.Inc(), helper.Inc(), helper.Counter)
	fmt.Println(helper.Platform())
	fmt.Println(helper.Describe(p))
	o := helper.Outer{}
	fmt.Println(o.T.Who())
	fmt.Println("main counter:", Counter)
	fmt.Println("helper counter:", helper.Counter)
	var t T
	fmt.Println(t.Who())
}
