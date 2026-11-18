package main

// Error() blocks (sleeps) while the crash report is rendered: gc renders
// it with the world still running, so the report completes (exit 2).

import (
	"fmt"
	"time"
)

type E struct{}

func (E) Error() string {
	time.Sleep(10 * time.Millisecond)
	return "slept"
}

func main() {
	fmt.Println("start")
	panic(E{})
}
