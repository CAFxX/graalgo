package main

import "fmt"

// 49_sscanf_scanner_error: fmt.Sscanf must return the Scanner error.
//
// Regression program for the F2 deferred-repack fix
// (REVIEW-2026-11-19-f2-sscanf-00bbc369, fixup F1). fmt.Sscanf funnels
// through doScanf, whose named (numProcessed, err) results are written by
// a deferred errorHandler after a bare return; before the fix the
// deferred write to err was silently dropped and Sscanf returned
// err == nil for every failing scan. gc prints "err nil: false" plus the
// error; a backend that drops the repacked value prints "err nil: true".
// The second case pins the success path (err == nil, n == 123) so a
// backend that always reports an error fails too.
//
// Deterministic by construction (fixed input, no maps, no concurrency,
// no wall clock); verified byte-identical to gc 1.27.1.

func main() {
	var n int
	_, err := fmt.Sscanf("abc", "%d", &n)
	fmt.Println("err nil:", err == nil)
	fmt.Println("err:", err)

	var m int
	_, err = fmt.Sscanf("123", "%d", &m)
	fmt.Println("n:", m)
	fmt.Println("err nil:", err == nil)
	fmt.Println("err:", err)
}
