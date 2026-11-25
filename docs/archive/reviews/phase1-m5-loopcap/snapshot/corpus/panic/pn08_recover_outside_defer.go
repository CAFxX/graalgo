package main

func main() {
	// 1. top-level call, no panic in flight.
	nil1 := recover() == nil
	// 2. deferred function, no panic in flight.
	nil2 := false
	func() {
		defer func() {
			nil2 = recover() == nil
		}()
	}()
	// 3. evaluated as a defer argument (at defer-statement time, no panic).
	nil3 := false
	// probe
	defer func(v any) { nil3 = v == nil }(recover())
	_ = nil1
	_ = nil2
	_ = nil3
}

// observe: nil1 nil2 nil3
