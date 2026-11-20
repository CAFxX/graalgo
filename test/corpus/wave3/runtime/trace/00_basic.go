package main

// L11 (REVIEW-2026-11-18-05): runtime/trace — zero corpus imports
// before this program. Execution tracing to a buffer: Start/Stop,
// IsEnabled, a second Start while tracing (error path), and a
// region + log inside a trace. Only sizes and booleans are printed
// (trace bytes carry timestamps).

import (
	"bytes"
	"context"
	"fmt"
	"runtime/trace"
)

func main() {
	fmt.Printf("enabled-before=%v\n", trace.IsEnabled())
	var buf bytes.Buffer
	if err := trace.Start(&buf); err != nil {
		fmt.Printf("start-err=%v\n", err != nil)
		return
	}
	fmt.Printf("enabled-during=%v\n", trace.IsEnabled())
	// A second Start while tracing is an error.
	err := trace.Start(&buf)
	fmt.Printf("second-start-err=%v\n", err != nil)
	// A region and a log entry inside the trace.
	trace.WithRegion(context.Background(), "demo-region", func() {
		trace.Log(context.Background(), "demo", "hello trace")
		sum := 0
		for i := 0; i < 1000; i++ {
			sum += i
		}
		fmt.Printf("work-sum=%d\n", sum)
	})
	trace.Stop()
	fmt.Printf("enabled-after=%v\n", trace.IsEnabled())
	fmt.Printf("trace-bytes-positive=%v\n", buf.Len() > 0)
	// Starting again after Stop works.
	var buf2 bytes.Buffer
	err = trace.Start(&buf2)
	fmt.Printf("restart-err=%v\n", err != nil)
	trace.Stop()
	fmt.Printf("restart-bytes-positive=%v\n", buf2.Len() > 0)
}
