package main

func main() {
	stage := 0
	// probe
	defer func() { stage = stage*10 + 3 }()
	defer func() {
		r := recover()
		if r != nil {
			stage = stage*10 + 2
		}
	}()
	defer func() { stage = stage*10 + 1 }()
	stage = 7
	panic("x")
	_ = stage
}

// observe: stage
