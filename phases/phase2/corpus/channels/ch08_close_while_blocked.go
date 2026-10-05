package main

import "fmt"

func main() {
	ch := make(chan int)
	done := make(chan bool)
	go func() {
		v, ok := <-ch
		fmt.Println(v, ok)
		done <- true
	}()
	close(ch)
	<-done
}
