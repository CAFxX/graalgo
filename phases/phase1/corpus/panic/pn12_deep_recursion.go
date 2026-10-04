package main

func main() {
	depth := 0
	maxDepth := 0
	recovered := false
	// probe
	defer func() {
		r := recover()
		recovered = r != nil
	}()
	var f func(int)
	f = func(n int) {
		depth++
		if depth > maxDepth {
			maxDepth = depth
		}
		if n <= 0 {
			panic("deep")
		}
		f(n - 1)
		depth--
	}
	f(10000)
	depth = -1
	_ = maxDepth
	_ = recovered
	_ = depth
}

// observe: maxDepth recovered depth
