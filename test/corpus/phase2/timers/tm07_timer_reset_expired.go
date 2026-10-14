package main

import (
	"fmt"
	"time"
)

func main() {
	t := time.NewTimer(20 * time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	<-t.C
	fmt.Println("reset:", t.Reset(50*time.Millisecond))
	<-t.C
	fmt.Println("fired")
}
