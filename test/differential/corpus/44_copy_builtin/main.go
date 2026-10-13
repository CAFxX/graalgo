package main

import "fmt"

func main() {
	src := []int{1, 2, 3, 4, 5}
	dst := make([]int, 3)
	n := copy(dst, src)
	fmt.Println(n, dst)
	// overlapping copy
	copy(src[1:], src[:4])
	fmt.Println(src)
	var s string
	_ = s
	sb := []byte("hello")
	fmt.Println(string(sb[:2]))
}
