package a

import "example.com/initorder/c"

var A = c.C + "a"

func init() { println("a init", A) }

func Twice(x int) int { return x * 2 }
