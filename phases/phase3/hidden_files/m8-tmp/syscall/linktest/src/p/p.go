package p

import _ "unsafe"

//go:linkname f other.G
func f() int

func Call() int { return f() }
