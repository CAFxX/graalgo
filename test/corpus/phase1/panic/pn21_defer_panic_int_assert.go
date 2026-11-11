package main

func main() {
	got := 0
	// probe
	defer func() {
		r := recover()
		if v, ok := r.(int); ok {
			got = v
		}
	}()
	defer panic(42)
	_ = got
}

// observe: got
