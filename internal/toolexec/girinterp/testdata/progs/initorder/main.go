package main

import (
	"example.com/initorder/a"
	"example.com/initorder/b"
)

// Package-level variable dependency order and init order across packages
// (gc: dependencies first; among ready packages, import-path order).

var (
	z = y + 1
	y = f("y", x*2)
	x = f("x", 1)
)

var w1, w2 = pair()

func pair() (int, int) { println("pair"); return 1, 2 }

func f(name string, v int) int {
	println("main var", name, v)
	return v
}

func init() { println("main init 1", z) }
func init() { println("main init 2") }

func main() {
	println("main", a.A, b.B, w1+w2)
	println(a.Twice(b.B))
}
