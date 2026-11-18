package main

func main() {
	ch := make(chan int)
	println("before")
	ch <- 1
	println("unreachable")
}
