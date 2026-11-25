package main

func main() {
	x := 1
	seen := 0
	// probe
	defer func(v int) { seen = v }(x)
	x = 2
	_ = seen
	_ = x
}

// observe: seen x
