package main

import "fmt"

func answer() int { return 42 }

var t = answer()
var u = t * 2

func main() {
	fmt.Println(t, u)
}
