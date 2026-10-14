package main

import "errors"

// BUG-MODPROBE-010: a multi-result call nested in argument position at
// package scope (var x = f(g())) hoists the inner call to a synthesized
// package-level temp global, emitted immediately before the initializer.
// General mechanism: works for any package, any user-defined multi-result
// function, at any nesting depth — never module-specific.
func parseLen(s string) (int, error) {
	if s == "" {
		return 0, errors.New("empty")
	}
	return len(s), nil
}

func mustLen(v int, err error) int {
	if err != nil {
		panic(err)
	}
	return v
}

var pkgMRArg = mustLen(parseLen("hello"))

func main() {
	println("pkgMRArg =", pkgMRArg)
}
