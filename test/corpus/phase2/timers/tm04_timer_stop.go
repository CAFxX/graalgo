package main

import (
	"fmt"
	"time"
)

func main() {
	t := time.NewTimer(200 * time.Millisecond)
	fmt.Println("stop:", t.Stop())
	select {
	case <-t.C:
		fmt.Println("FIRED (bug)")
	case <-time.After(300 * time.Millisecond):
		fmt.Println("no fire")
	}
}
