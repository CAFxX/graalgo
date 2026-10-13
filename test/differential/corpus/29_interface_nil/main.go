package main

import "fmt"

func main() {
	var p *int
	var i any = p // typed nil: interface is non-nil
	fmt.Println(i == nil)
	var j any
	fmt.Println(j == nil)
	var s []int
	var k any = s
	fmt.Println(k == nil)
	m := map[string]any{"nil": nil, "typed": p}
	fmt.Println(m["nil"] == nil, m["typed"] == nil)
}
