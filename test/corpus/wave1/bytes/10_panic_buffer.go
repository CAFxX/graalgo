package main

import (
	"bytes"
	"fmt"
)

func main() {
	// Grow with negative n panics with ErrTooLarge.
	// (Buffer.Truncate/Next also panic on out-of-range input, but they
	// write the int8 lastRead field first: runtime crash, see README.)
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("panic:", r)
		}
	}()
	b := bytes.NewBufferString("hi")
	b.Grow(-1)
	fmt.Println("no panic")
}
