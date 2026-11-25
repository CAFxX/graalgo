// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package runtime is graalgo's facade of the Go runtime
// (docs/arch/toolexec-design.md D8, §5.6): it replaces upstream's package
// in the GOROOT graalgo ships. It declares upstream's exported API (the
// identifiers code outside the runtime family names) and nothing else. Declarations and documentation are
// upstream's. A function without a body is an intrinsic that the graalgo
// linker resolves by symbol name against the intrinsic registry
// (internal/toolexec/intrinsics/registry.txt); a reachable one without an
// entry fails the link, naming the symbol.
//
// Language operations (allocation, maps, channels, defer, panics, goroutine
// start) are nodes of the backend and never appear here, and neither do the
// functions upstream's runtime provides to other packages by //go:linkname:
// those packages declare them, and the linker resolves the declarations
// against the same registry — except where upstream's body is plain Go over
// the facade's own functions, which the facade then pushes by //go:linkname
// as upstream does: coroutines for iter (coro.go), the legacy random
// generators (rand.go), the runtime/debug knobs (debug.go) and the
// runtime/trace hooks (trace.go).
//
// The exported error types, TypeAssertionError and PanicNilError, are
// upstream's Go source (error.go), which the backend's panics instantiate;
// the unexported ones (errorString, plainError, boundsError) are still the
// backend's own types.
package runtime

import (
	"internal/goarch"
	"internal/goos"
)

// Compiler is the name of the compiler toolchain that built the
// running binary. It stays "gc": libraries branch on it as on a build tag,
// and graalgo compiles the sources gc compiles.
const Compiler = "gc"

// GOOS is the running program's operating system target.
const GOOS string = goos.GOOS

// GOARCH is the running program's architecture target.
const GOARCH string = goarch.GOARCH

// An Error identifies a run time error.
type Error interface {
	error

	// RuntimeError is a no-op function but
	// serves to distinguish types that are run time
	// errors from ordinary errors: a type is a
	// run time error if it has a RuntimeError method.
	RuntimeError()
}

// Gosched yields the processor, allowing other goroutines to run. It does not
// suspend the current goroutine, so execution resumes automatically.
func Gosched()

// GC runs a garbage collection and blocks the caller until the
// garbage collection is complete.
func GC()

// GOMAXPROCS sets the maximum number of CPUs that can be executing
// simultaneously and returns the previous setting. If n < 1, it does not
// change the current setting.
func GOMAXPROCS(n int) int

// NumCPU returns the number of logical CPUs usable by the current process.
func NumCPU() int

// Goexit terminates the goroutine that calls it. No other goroutine is affected.
// Goexit runs all deferred calls before terminating the goroutine. Because Goexit
// is not a panic, any recover calls in those deferred functions will return nil.
func Goexit()

// nanotime returns the monotonic clock in nanoseconds. Not exported;
// reached by //go:linkname, as upstream's is.
func nanotime() int64

// divideError and overflowError are the values math/bits panics with
// (//go:linkname in math/bits/bits_errors.go). Upstream initializes them
// from its errorString type; here they are the backend's own values for
// the same two run-time errors, so that a recovered panic is the same
// value whichever side raised it.
var (
	divideError   error
	overflowError error
)

func init() {
	divideError = runtimeError(0)
	overflowError = runtimeError(1)
}

// runtimeError returns the backend's runtime.Error value for one of the
// run-time errors a Go package raises itself: 0 integer divide by zero,
// 1 integer overflow. Facade-private; retired when error.go is kept from
// upstream.
func runtimeError(code int) error

// staticuint64s is upstream's table of the small integers (runtime/iface.go),
// which reflect reads through getStaticuint64s (//go:linkname in
// reflect/value.go) to make the data word of a small integer without
// allocating.
var staticuint64s [256]uint64

func init() {
	for i := range staticuint64s {
		staticuint64s[i] = uint64(i)
	}
}

func getStaticuint64s() *[256]uint64 { return &staticuint64s }

// fcntl is the system call internal/syscall/unix reaches by //go:linkname
// (upstream runtime/os_linux.go): the result and the errno.
func fcntl(fd, cmd, arg int32) (ret int32, errno int32)
