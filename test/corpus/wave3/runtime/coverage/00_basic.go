package main

// L11 (REVIEW-2026-11-18-05): runtime/coverage — zero corpus imports
// before this program. Coverage-data writing without a coverage
// build: WriteCounters and WriteMeta both report errors (the binary
// was not built with -cover), and ClearCounters reports the same.
// Only error booleans are printed — no coverage data exists here.

import (
	"bytes"
	"fmt"
	"runtime/coverage"
)

func main() {
	var buf bytes.Buffer
	err := coverage.WriteCounters(&buf)
	fmt.Printf("writecounters-err=%v bytes=%d\n", err != nil, buf.Len())
	buf.Reset()
	err = coverage.WriteMeta(&buf)
	fmt.Printf("writemeta-err=%v bytes=%d\n", err != nil, buf.Len())
	err = coverage.ClearCounters()
	fmt.Printf("clearcounters-err=%v\n", err != nil)
	// The directory variants take a path and fail the same way.
	err = coverage.WriteCountersDir("/nonexistent-dir-1805")
	fmt.Printf("writecountersdir-err=%v\n", err != nil)
	err = coverage.WriteMetaDir("/nonexistent-dir-1805")
	fmt.Printf("writemetadir-err=%v\n", err != nil)
}
