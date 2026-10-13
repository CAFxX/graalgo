package main

func inner() {
	panic("deep boom")
}

func middle() {
	inner()
}

func outer() {
	middle()
}

func main() {
	outer()
}
