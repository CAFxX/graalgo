package main

func main() {
	ch := make(chan int)
	close(ch)
	select {
	case ch <- 1:
		println("sent")
	default:
		println("default")
	}
}
