package main

import "fmt"

func inner() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("inner recovered:", r)
			panic("re-raised")
		}
	}()
	panic("original")
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("outer recovered:", r)
		}
	}()
	inner()
	fmt.Println("unreachable")
}
