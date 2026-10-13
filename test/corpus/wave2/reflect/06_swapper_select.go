package main

import (
	"fmt"
	"reflect"
)

func main() {
	// Swapper via Slice.
	s := []int{3, 1, 2}
	reflect.Swapper(s)(0, 2)
	fmt.Println(s)
	// Copy.
	dst := []int{0, 0, 0}
	n := reflect.Copy(reflect.ValueOf(dst), reflect.ValueOf(s))
	fmt.Println(n, dst)
	// Select with a recv case.
	ch := make(chan int, 1)
	ch <- 42
	cases := []reflect.SelectCase{
		{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)},
		{Dir: reflect.SelectDefault},
	}
	chosen, recv, ok := reflect.Select(cases)
	fmt.Println(chosen, recv.Int(), ok)
}
