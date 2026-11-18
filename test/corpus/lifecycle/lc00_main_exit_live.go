package main

import "fmt"

func main() {
	go func() {
		for {
		}
	}()
	go func() {
		select {}
	}()
	c := make(chan int)
	go func() { <-c }()
	fmt.Println("main done")
}
