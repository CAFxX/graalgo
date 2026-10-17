package main

import "encoding/binary"

// Pins the user-package path of etaExpandMRFuncValues (independent review
// 2026-10-17, R2 P1-3/A1): a package-level `var uvarintFn = binary.Uvarint`
// where Uvarint is multi-result keeps its declaration — the global exists,
// its initializer lowers to the pair-returning __-helper ident, and the
// call through the variable desugars via the linked pair info. (encoding/
// binary is used instead of os to keep the golden under the 1MB limit; the
// mechanism is identical for any table-driven multi-result function.)
var uvarintFn = binary.Uvarint

func main() {
	x, n := uvarintFn([]byte{0x81, 0x01})
	println("x =", x, "n =", n)
}
