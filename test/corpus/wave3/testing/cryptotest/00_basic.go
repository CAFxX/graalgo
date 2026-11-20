package main

// L11 (REVIEW-2026-11-18-05): testing/cryptotest — zero corpus
// imports before this program. The package has exactly one public
// function, SetGlobalRandom, and it panics unless the binary is a
// test binary (testing.Testing() is false under `go build`). This is
// therefore a panic program: the expected first stderr line is in
// 00_basic.panic, per the corpus panic-program convention.

import (
	"testing"
	"testing/cryptotest"
)

func main() {
	cryptotest.SetGlobalRandom(&testing.T{}, 42)
}
