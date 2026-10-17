package main

import "encoding/binary"

// Pins multi-name ValueSpec handling for package-level multi-result
// function values (independent review 2026-10-17, R2 P1-3): in
// `var uvarintFn, answer = binary.Uvarint, 42` only the aliased name is
// linked to the pair info — `answer` keeps its plain declaration and
// initializer. (encoding/binary stands in for os.Lstat to keep the golden
// under the 1MB limit; the mechanism is identical.)
var uvarintFn, answer = binary.Uvarint, 42

func main() {
	x, n := uvarintFn([]byte{0x01})
	println("x =", x, "n =", n, "answer =", answer)
}
