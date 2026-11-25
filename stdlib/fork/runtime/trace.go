// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// What upstream's runtime provides to runtime/trace by //go:linkname
// (trace.go, tracetime.go, traceruntime.go). graalgo has no execution
// tracer (StartTrace reports it, mprof.go): there is never a trace to
// advance or annotate, and the clock is nanotime in upstream's units.

package runtime

import _ "unsafe" // for go:linkname

// traceTimeDiv is upstream's divisor of the trace clock on amd64.
const traceTimeDiv = 64

// traceClockNow returns the current trace clock value.
func traceClockNow() uint64 {
	return uint64(nanotime()) / traceTimeDiv
}

//go:linkname traceClockUnitsPerSecond runtime/trace.runtime_traceClockUnitsPerSecond
func traceClockUnitsPerSecond() uint64 {
	return 1e9 / traceTimeDiv
}

// traceAdvance would move the tracer to its next generation.
func traceAdvance(stopTrace bool) {}

//go:linkname trace_userTaskCreate runtime/trace.userTaskCreate
func trace_userTaskCreate(id, parentID uint64, taskType string) {}

//go:linkname trace_userTaskEnd runtime/trace.userTaskEnd
func trace_userTaskEnd(id uint64) {}

//go:linkname trace_userRegion runtime/trace.userRegion
func trace_userRegion(id, mode uint64, name string) {}

//go:linkname trace_userLog runtime/trace.userLog
func trace_userLog(id uint64, category, message string) {}
