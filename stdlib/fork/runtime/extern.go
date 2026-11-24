// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The exported API of upstream's extern.go and debug.go that code outside
// the runtime family uses: declarations and documentation are upstream's,
// the functions are intrinsics (see runtime.go) or Go over intrinsics.

package runtime

// Caller reports file and line number information about function invocations on
// the calling goroutine's stack. The argument skip is the number of stack frames
// to ascend, with 0 identifying the caller of Caller. (For historical reasons the
// meaning of skip differs between Caller and [Callers].) The return values report
// the program counter, the file name (using forward slashes as path separator, even
// on Windows), and the line number within the file of the corresponding call.
// The boolean ok is false if it was not possible to recover the information.
func Caller(skip int) (pc uintptr, file string, line int, ok bool) {
	var rpc [1]uintptr
	n := callers(skip+1, rpc[:])
	if n < 1 {
		return
	}
	frame, _ := CallersFrames(rpc[:]).Next()
	return frame.PC, frame.File, frame.Line, frame.PC != 0
}

// Callers fills the slice pc with the return program counters of function invocations
// on the calling goroutine's stack. The argument skip is the number of stack frames
// to skip before recording in pc, with 0 identifying the frame for Callers itself and
// 1 identifying the caller of Callers.
// It returns the number of entries written to pc.
//
// To translate these PCs into symbolic information such as function
// names and line numbers, use [CallersFrames]. CallersFrames accounts
// for inlined functions and adjusts the return program counters into
// call program counters. Iterating over the returned slice of PCs
// directly is discouraged, as is using [FuncForPC] on any of the
// returned PCs, since these cannot account for inlining or return
// program counter adjustment.
func Callers(skip int, pc []uintptr) int {
	if len(pc) == 0 {
		return 0
	}
	return callers(skip, pc)
}

// callers fills pcbuf with the return pcs of the calling goroutine's Go
// frames, skipping skip of them; skip 0 is the caller of callers. A frame of
// a function without a Go body (an intrinsic) is not a Go frame.
//
//go:noescape
func callers(skip int, pcbuf []uintptr) int

// funcInfo reports the name and entry pc of the function containing pc;
// entry 0 if none does.
func funcInfo(pc uintptr) (name string, entry uintptr)

// pcFileLine reports the file and line of pc ("", 0 if no function
// contains it).
func pcFileLine(pc uintptr) (file string, line int)

// GOROOT returns the root of the Go tree. It uses the
// GOROOT environment variable, if set at process start,
// or else the root used during the Go build.
//
// Deprecated: The root used during the Go build will not be
// meaningful if the binary is copied to another machine.
// Use the system path to locate the “go” binary, and use
// “go env GOROOT” to find its GOROOT.
func GOROOT() string

// Version returns the Go tree's version string.
// It is either the commit hash and date at the time of the build or,
// when possible, a release tag like "go1.3".
func Version() string

// NumGoroutine returns the number of goroutines that currently exist.
func NumGoroutine() int

// Stack formats a stack trace of the calling goroutine into buf
// and returns the number of bytes written to buf.
// If all is true, Stack formats stack traces of all other goroutines
// into buf after the trace for the current goroutine.
func Stack(buf []byte, all bool) int
