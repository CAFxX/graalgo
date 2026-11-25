package main

func main() {
	sum := 0
	for i := 1; i <= 100; i++ {
		sum += i
	}
	prod := 1
	for i := 1; i <= 10; i++ {
		prod *= i
	}
	// empty body, decrementing
	n := 10
	for i := n; i > 0; i-- {
		n = i - 1
	}
	_ = sum
	_ = prod
	_ = n
}

// observe: sum prod n
