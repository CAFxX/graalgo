package main

func main() {
	order := 0
	// probe
	defer func() { order = order*10 + 1 }()
	defer func() { order = order*10 + 2 }()
	defer func() { order = order*10 + 3 }()
	_ = order
}

// observe: order
