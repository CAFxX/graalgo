package main

import "fmt"

// _smoke 00: synthetic fmt surface (Print/Println/Printf + the Sprint
// family). Deterministic output; proves the harness drives a
// multi-symbol synthetic package end to end.
func main() {
	fmt.Print("a", 1, true, "\n")
	fmt.Println("b", 2, false)
	fmt.Printf("%s=%d %v %x\n", "c", 3, -4, 255)
	fmt.Println(fmt.Sprint("d", 5))
	fmt.Println(fmt.Sprintf("e=%d", 6))
	fmt.Println(fmt.Sprintln("f", 7))
	fmt.Println(fmt.Errorf("g%d", 8))
}
