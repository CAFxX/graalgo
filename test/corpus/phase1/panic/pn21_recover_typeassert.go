package main

// bug-019 (review remediation): a runtime panic (integer divide by zero)
// throws a bare GoErrorValue; recover() must box it as a Go any via the
// inferred descriptor. Before the inference the recovered value was not an
// interface value, so the type assertion and the type switch below crashed
// (GoInterface.narrow's blind cast); after it they run like gc's.
func main() {
	var r any
	ok := false
	tag := 0
	// probe
	func() {
		defer func() { r = recover() }()
		x, y := 1, 0
		_ = x / y
	}()
	_, ok = r.(any)
	switch r.(type) {
	case nil:
		tag = 1
	default:
		tag = 2
	}
	_, _ = ok, tag
}

// observe: ok tag
