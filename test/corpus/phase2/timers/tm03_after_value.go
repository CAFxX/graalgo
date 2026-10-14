package main

import (
	"fmt"
	"time"
)

func main() {
	<-time.After(50 * time.Millisecond)
	fmt.Println("fired")
}
