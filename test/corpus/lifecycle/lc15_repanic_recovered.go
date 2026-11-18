package main

import "fmt"

func main() {
	defer func() {
		r := recover()
		fmt.Println("recovered:", r)
		panic(fmt.Sprint("second: ", r))
	}()
	panic("first")
}
