package main

func main() {
	log := 0
	final := ""
	// probe
	defer func() { log = log*10 + 1 }()
	defer func() {
		r := recover()
		if s, ok := r.(string); ok {
			final = s
		}
		log = log*10 + 2
	}()
	defer func() {
		log = log*10 + 3
		panic("second")
	}()
	panic("first")
	_ = log
	_ = final
}

// observe: log final
