package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "about to exit")
	fmt.Println("stdout line")
	os.Exit(3)
}
