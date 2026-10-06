package main

import "fmt"

func cleanup() {
	fmt.Println("cleaned")
}

func main() {
	defer cleanup()
	panic("with defer")
}
