package main

func main() {
	ri := -1
	rr := ""
	rb := false
	rf := 0.0
	try := func(v any) {
		defer func() {
			r := recover()
			switch t := r.(type) {
			case int:
				ri = t
			case string:
				rr = t
			case bool:
				rb = t
			case float64:
				rf = t
			}
		}()
		panic(v)
	}
	try(42)
	try("hello")
	try(true)
	try(1.5)
	_ = ri
	_ = rr
	_ = rb
	_ = rf
	// probe
}

// observe: ri rr rb rf
