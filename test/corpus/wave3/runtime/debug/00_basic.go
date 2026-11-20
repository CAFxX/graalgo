package main

// L11 (REVIEW-2026-11-18-05): runtime/debug — zero corpus imports
// before this program. Runtime tuning knobs and build info:
// SetGCPercent / SetMaxStack / SetMaxThreads / SetMemoryLimit
// round-trips, FreeOSMemory, and ReadBuildInfo.

import (
	"fmt"
	"runtime/debug"
	"sort"
)

func main() {
	// SetGCPercent returns the previous value; restore it after.
	prev := debug.SetGCPercent(50)
	fmt.Printf("gcpercent-prev=%d\n", prev)
	restored := debug.SetGCPercent(prev)
	fmt.Printf("gcpercent-roundtrip=%v\n", restored == 50)
	// MaxStack: 0 means the default; set a value and restore.
	prevStack := debug.SetMaxStack(1 << 28)
	fmt.Printf("maxstack-prev=%d\n", prevStack)
	debug.SetMaxStack(prevStack)
	// MaxThreads: 0 means the default; set and restore.
	prevThreads := debug.SetMaxThreads(2000)
	fmt.Printf("maxthreads-prev=%d\n", prevThreads)
	debug.SetMaxThreads(prevThreads)
	// Memory limit: -1 disables, 0 is the default.
	prevLimit := debug.SetMemoryLimit(1 << 30)
	fmt.Printf("memlimit-prev=%d\n", prevLimit)
	debug.SetMemoryLimit(prevLimit)
	// FreeOSMemory is a no-op for output; it must not panic.
	debug.FreeOSMemory()
	fmt.Printf("freeosmemory-ok=true\n")
	// Build info of the running binary.
	bi, ok := debug.ReadBuildInfo()
	fmt.Printf("buildinfo-ok=%v\n", ok)
	if ok {
		fmt.Printf("buildinfo-goversion=%s main-path=%q\n", bi.GoVersion, bi.Main.Path)
		keys := make([]string, 0, len(bi.Settings))
		for _, s := range bi.Settings {
			keys = append(keys, s.Key)
		}
		sort.Strings(keys)
		fmt.Printf("buildinfo-settings=%q\n", keys)
		fmt.Printf("buildinfo-deps=%d\n", len(bi.Deps))
	}
	// Traceback level: set to "single" (last statement; no restore needed).
	debug.SetTraceback("single")
	fmt.Printf("traceback-set=true\n")
}
