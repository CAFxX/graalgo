package main

func main() {
	strOk := 0
	// probe
	defer func() {
		r := recover()
		if _, ok := r.(string); ok {
			strOk = 1
		}
	}()
	n := -1
	_ = make([]int, n)
	_ = strOk
}

// observe: strOk
