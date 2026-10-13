package main

import "fmt"

// P3-M3 R1 P0-1 regression: the type-assertion implements check must be
// signature-aware. Bad has a method NAMED Write but with the wrong
// signature — a.(Writer) must fail (gc parity). Good implements Writer
// with a differently-named parameter (param names are not significant),
// and Cross proves byte/uint8/alias spellings compare identical.
type Writer interface{ Write([]byte) int }

type Bad struct{}

func (b Bad) Write(s string) int { return 1 }

type Good struct{}

func (g Good) Write(p []byte) int { return len(p) }

type B = []byte

type Cross struct{}

func (c Cross) Write(s B) int { return 7 }

func main() {
	var a any = Bad{}
	_, ok := a.(Writer)
	fmt.Println(ok) // false: same name, wrong signature

	var g any = Good{}
	w, ok := g.(Writer)
	fmt.Println(ok, w.Write([]byte("hi"))) // true 2

	var c any = Cross{}
	w2, ok := c.(Writer)
	fmt.Println(ok, w2.Write(nil)) // true 7: alias expanded

	// Single-valued form panics with the interface-conversion message.
	func() {
		defer func() {
			r := recover()
			fmt.Println(r != nil)
		}()
		_ = a.(Writer)
	}()
}
