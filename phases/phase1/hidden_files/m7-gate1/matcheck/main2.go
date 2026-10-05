package main

import "fmt"

func get(a [2][2]int) [2]int {
	return a[0]
}

func main() {
	b := [2][2]int{{1, 2}, {3, 4}}
	fmt.Println(get(b))
}
