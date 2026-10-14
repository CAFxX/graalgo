package main

import (
	"fmt"
	"time"
)

func main() {
	tk := time.NewTicker(50 * time.Millisecond)
	for i := 0; i < 3; i++ {
		<-tk.C
		fmt.Println("tick")
	}
	tk.Stop()
	fmt.Println("stopped")
}
