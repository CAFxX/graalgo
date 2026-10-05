package main

import (
	"example.com/twopkg/helper"
	"fmt"
)

func main() {
	fmt.Println(helper.Greeting)
	fmt.Println(helper.Add(40, 2))
	p := helper.Point{X: 1, Y: 2}
	fmt.Println(p.String())
	fmt.Println(helper.Inc(), helper.Inc(), helper.Counter)
	fmt.Println(helper.Platform())
	fmt.Println(helper.Describe(p))
}
