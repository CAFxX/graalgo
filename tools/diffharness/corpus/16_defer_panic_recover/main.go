package main

import "fmt"

func mayPanic(n int) (r int) {
	defer func() {
		if e := recover(); e != nil {
			fmt.Println("recovered:", e)
			r = -1
		}
	}()
	defer fmt.Println("cleanup", n)
	if n < 0 {
		panic("negative!")
	}
	return n * 2
}

func main() {
	fmt.Println(mayPanic(21))
	fmt.Println(mayPanic(-5))
}
