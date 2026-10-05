package main

import "fmt"

func main() {
	a := [2]int{1, 2}
	fmt.Println(a)
	b := [2][2]int{{1, 2}, {3, 4}}
	fmt.Println(b[0])
}
