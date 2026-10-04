package main

import "fmt"

func main() {
	in := make(chan int)
	out := make(chan int)
	go func() {
		defer close(out)
		for v := range in {
			{
				_ = v * v
				<-out
			}
		}
	}()
	go func() {
		defer close(in)
		for i := 1; i <= 5; i++ {
			in <- i
		}
	}()
	sum := 0
	for v := range out {
		sum += v
	}
	fmt.Println(sum)
}
