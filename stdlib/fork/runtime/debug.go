// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// What upstream's runtime provides to runtime/debug by //go:linkname
// (mgcpacer.go, rdebug.go, proc.go, mheap.go, mstats.go, runtime1.go,
// heapdump.go, debug.go). The collector is the JVM's: the knobs record and
// report their settings, as gc's do, but steer nothing; each body says what
// it does (docs/arch/limitations.md). setPanicOnFault is per goroutine and
// an intrinsic (registry.txt).

package runtime

import _ "unsafe" // for go:linkname

// The settings, with gc's defaults on 64-bit linux (GOGC and GOMEMLIMIT
// are not read: the JVM's heap flags size the heap).
var (
	debugGCPercent   int32   = 100
	debugMemoryLimit int64   = 1<<63 - 1
	debugMaxStack            = 1000000000
	debugMaxThreads          = 10000
	debugCrashFD     uintptr = ^uintptr(0)
	debugTraceback   string
)

//go:linkname debug_setGCPercent runtime/debug.setGCPercent
func debug_setGCPercent(in int32) (out int32) {
	out = debugGCPercent
	if in < 0 {
		in = -1
	}
	debugGCPercent = in
	return out
}

//go:linkname debug_setMemoryLimit runtime/debug.setMemoryLimit
func debug_setMemoryLimit(in int64) (out int64) {
	out = debugMemoryLimit
	if in >= 0 {
		debugMemoryLimit = in
	}
	return out
}

//go:linkname debug_setMaxStack runtime/debug.setMaxStack
func debug_setMaxStack(in int) (out int) {
	out = debugMaxStack
	debugMaxStack = in
	return out
}

//go:linkname debug_setMaxThreads runtime/debug.setMaxThreads
func debug_setMaxThreads(in int) (out int) {
	out = debugMaxThreads
	debugMaxThreads = in
	return out
}

// debug_freeOSMemory forces a collection; the JVM returns memory to the
// operating system on its own terms.
//
//go:linkname debug_freeOSMemory runtime/debug.freeOSMemory
func debug_freeOSMemory() {
	GC()
}

// readGCStats is upstream's (mstats.go readGCStats_m) over ReadMemStats:
// pauses (most recent first), pause ends, last GC, NumGC, PauseTotalNs.
//
//go:linkname readGCStats runtime/debug.readGCStats
func readGCStats(pauses *[]uint64) {
	var m MemStats
	ReadMemStats(&m)
	p := *pauses
	if cap(p) < len(m.PauseNs)+3 {
		panic("short slice passed to readGCStats")
	}
	n := m.NumGC
	if n > uint32(len(m.PauseNs)) {
		n = uint32(len(m.PauseNs))
	}
	p = p[:cap(p)]
	for i := uint32(0); i < n; i++ {
		j := (m.NumGC - 1 - i) % uint32(len(m.PauseNs))
		p[i] = m.PauseNs[j]
		p[n+i] = m.PauseEnd[j]
	}
	p[n+n] = m.LastGC
	p[n+n+1] = uint64(m.NumGC)
	p[n+n+2] = m.PauseTotalNs
	*pauses = p[:n+n+3]
}

// setTraceback records the level; graalgo's crash reports print the
// failing goroutine whatever it is.
//
//go:linkname setTraceback runtime/debug.SetTraceback
func setTraceback(level string) {
	debugTraceback = level
}

// runtime_debug_WriteHeapDump writes nothing: gc's heap dump format
// describes gc's heap, which graalgo does not have.
//
//go:linkname runtime_debug_WriteHeapDump runtime/debug.WriteHeapDump
func runtime_debug_WriteHeapDump(fd uintptr) {}

// setCrashFD records the descriptor and returns the previous one; crash
// reports still go to standard error.
//
//go:linkname setCrashFD
func setCrashFD(fd uintptr) uintptr {
	old := debugCrashFD
	debugCrashFD = fd
	return old
}

// modinfo is the main module's build information, set by the link as
// cmd/link sets it (graalgo-tool link, from importcfg's modinfo).
var modinfo string

//go:linkname debug_modinfo runtime/debug.modinfo
func debug_modinfo() string {
	return modinfo
}
