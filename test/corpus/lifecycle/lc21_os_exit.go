package main

import (
	"fmt"
	"os"
)

func main() {
	defer fmt.Println("not printed")
	go func() { select {} }()
	fmt.Print("partial line no newline")
	os.Exit(3)
}
