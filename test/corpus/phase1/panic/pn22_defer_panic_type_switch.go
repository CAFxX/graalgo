package main

func main() {
	got := 0
	// probe
	defer func() {
		switch t := recover().(type) {
		case string:
			_ = t
			got = 1
		case int:
			_ = t
			got = 2
		default:
			_ = t
			got = 3
		}
	}()
	defer panic("boom")
	_ = got
}

// observe: got
