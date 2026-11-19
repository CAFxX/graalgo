// math/big Scan: fmt.Sscanf into big.Float, big.Int and big.Rat.
// Covers Int verbs b/o/d/x/X/v, Float verbs f/v, Rat fractions, an
// invalid verb, an invalid (multi-byte) rune, and empty input.
// Deterministic by construction: fixed inputs, fixed outputs.
package main

import (
	"fmt"
	"math/big"
)

func scanInt(input, verb string) {
	var z big.Int
	n, err := fmt.Sscanf(input, verb, &z)
	fmt.Printf("int %s %q: n=%d err=%v val=%s\n", verb, input, n, err, z.String())
}

func scanFloat(input, verb string) {
	var z big.Float
	n, err := fmt.Sscanf(input, verb, &z)
	fmt.Printf("float %s %q: n=%d err=%v val=%s\n", verb, input, n, err, z.Text('g', -1))
}

func scanRat(input, verb string) {
	var z big.Rat
	n, err := fmt.Sscanf(input, verb, &z)
	fmt.Printf("rat %s %q: n=%d err=%v val=%s\n", verb, input, n, err, z.RatString())
}

func main() {
	// Int verbs: binary, octal, decimal, hex, base-detected.
	scanInt("1010", "%b")
	scanInt("17", "%o")
	scanInt("-42", "%d")
	scanInt("ff", "%x")
	scanInt("FF", "%X")
	scanInt("0x1f", "%v")
	// Float verbs.
	scanFloat("1.5", "%f")
	scanFloat("2.5e3", "%v")
	// Rat: fraction and decimal.
	scanRat("3/4", "%v")
	scanRat("1.5", "%g")
	// Invalid verb: Int has no %e, Rat has no %d.
	scanInt("10", "%e")
	scanRat("3/4", "%d")
	// Invalid rune: a multi-byte rune where a digit is expected
	// (byteReader.ReadByte rejects size != 1).
	scanInt("é", "%d")
	// Empty input: EOF propagates from the scanner.
	scanInt("", "%d")
}
