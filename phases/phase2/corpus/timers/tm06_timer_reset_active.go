package main

import (
	"fmt"
	"time"
)

func main() {
	t := time.NewTimer(500 * time.Millisecond)
	fmt.Println("reset:", t.Reset(20*time.Millisecond))
	<-t.C
	fmt.Println("fired")
}
