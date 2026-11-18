package main

func main() {
	defer func() {
		panic("second")
	}()
	panic("first")
}
