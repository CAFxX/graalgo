package main

// new: allocates zero value. (Explicit &x/*p are WS D; new is the builtin.)
func main() {
	p := new(int)
	// Zero value via new; assignment through pointer is WS D, so we just
	// verify allocation succeeded by passing to a function.
	println("allocated:", p != nil)
}
