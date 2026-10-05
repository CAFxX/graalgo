package main

import "fmt"

func main() {
	func() {
		defer func() {
			r := recover()
			fmt.Printf("recovered: %v (%T)\n", r, r)
		}()
		panic(nil)
	}()
	fmt.Println("after")
}
