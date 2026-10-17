package main

import "fmt"

// fmt via the real package (compiled from GOROOT since real-fmt2
// retired the D4 Java stub, 2026-10-16): the seven pure functions.
func main() {
	fmt.Print("a", 1)
	fmt.Printf("%d+%d=%d\n", 1, 2, 3)
	fmt.Println("x", "y")
	s := fmt.Sprint("p", 5)
	fmt.Println(s)
	t := fmt.Sprintf("%05d", 42)
	fmt.Println(t)
	u := fmt.Sprintln("m", 7)
	fmt.Print(u)
	err := fmt.Errorf("oops %d", 7)
	fmt.Println(err)
}
