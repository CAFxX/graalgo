package main

import (
	"fmt"
	"time"
)

func main() {
	t := time.NewTimer(20 * time.Millisecond)
	time.Sleep(100 * time.Millisecond) // fired, not drained
	fmt.Println("stop:", t.Stop())     // Go 1.23+: true, drains the pending value
	select {
	case <-t.C:
		fmt.Println("value leaked (bug)")
	default:
		fmt.Println("drained")
	}
}
