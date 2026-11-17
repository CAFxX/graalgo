// 59_recover_named_results: a function that panics, recovers in a deferred
// call and returns normally must return its NAMED results as the deferred
// calls left them (Go spec, "Handling panics"). Split out of
// 57_defer_order: graalgo returns the zero values ("0 <nil>").
package main

import "fmt"

func named() (n int, err error) {
	defer func() {
		n *= 10
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered: %v", r)
		}
	}()
	defer func() { n += 3 }()
	n = 1
	panic("boom")
}

func plain() (s string) {
	defer func() {
		if recover() != nil {
			s = "set by defer"
		}
	}()
	s = "set before panic"
	panic("x")
}

func main() {
	fmt.Println(named())
	fmt.Println(plain())
}
