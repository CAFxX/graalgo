package main

func main() {
	f := func() (r int) {
		defer func() { r = 42 }()
		r = 1
		return
	}
	got := f()
	// probe
	_ = got
}

// observe: got
