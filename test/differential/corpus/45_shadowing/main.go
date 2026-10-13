package main

import "fmt"

func main() {
	x := "outer"
	{
		x := "inner"
		fmt.Println(x)
		{
			x := "innermost"
			fmt.Println(x)
		}
		fmt.Println(x)
	}
	fmt.Println(x)
	y := 1
	if y := 2; y > 1 {
		fmt.Println("shadowed", y)
	}
	fmt.Println("outer", y)
}
