package main

// L11 (REVIEW-2026-11-18-05): crypto/fips140 — zero corpus imports before
// this program. Module status queries: Enabled, Enforced, Version, and
// WithoutEnforcement runs the closure with enforcement suspended.

import (
	"crypto/fips140"
	"fmt"
)

func main() {
	fmt.Printf("enabled=%v\n", fips140.Enabled())
	fmt.Printf("enforced=%v\n", fips140.Enforced())
	fmt.Printf("version=%q\n", fips140.Version())
	ran := false
	fips140.WithoutEnforcement(func() {
		ran = true
		fmt.Printf("inside-without-enforcement=%v\n", fips140.Enforced())
	})
	fmt.Printf("without-enforcement-ran=%v\n", ran)
	fmt.Printf("enforced-after=%v\n", fips140.Enforced())
}
