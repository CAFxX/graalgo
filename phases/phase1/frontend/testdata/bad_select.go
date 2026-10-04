package main

func main() {
	x := 0
	select {
	default:
		x++
	}
	_ = x
}
