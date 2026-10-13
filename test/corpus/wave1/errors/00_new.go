package main

import "errors"

// er00: errors.New — message rendering, distinct values, ErrUnsupported.
func main() {
	e1 := errors.New("boom")
	e2 := errors.New("boom")
	println(e1.Error())
	// Each call to New returns a distinct error value.
	println(e1 == e2)
	println(e1 == e1)
	empty := errors.New("")
	println("[" + empty.Error() + "]")
	println(errors.ErrUnsupported.Error())
	println(errors.Is(errors.ErrUnsupported, errors.ErrUnsupported))
	var nilErr error
	println(nilErr == nil)
	println(errors.New("x") == nil)
}
