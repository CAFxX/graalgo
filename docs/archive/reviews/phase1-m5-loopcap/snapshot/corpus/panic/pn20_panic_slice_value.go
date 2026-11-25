package main

func main() {
	saw := 0
	// probe
	defer func() {
		r := recover()
		if r != nil {
			saw = 1
		}
	}()
	panic([]int{1, 2})
	_ = saw
}

// observe: saw
