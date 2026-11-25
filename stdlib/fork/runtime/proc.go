// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The thread, cgo and debugging API of upstream's proc.go, cgo.go,
// traceback.go and pinner.go.

package runtime

import "unsafe"

// LockOSThread wires the calling goroutine to its current operating system thread.
// The calling goroutine will always execute in that thread,
// and no other goroutine will execute in it,
// until the calling goroutine has made as many calls to
// [UnlockOSThread] as to LockOSThread.
// If the calling goroutine exits without unlocking the thread,
// the thread will be terminated.
//
// All init functions are run on the startup thread. Calling LockOSThread
// from an init function will cause the main function to be invoked on
// that thread.
//
// A goroutine should call LockOSThread before calling OS services or
// non-Go library functions that depend on per-thread state.
//
// graalgo: a goroutine has no per-thread state that Go code can reach (no
// cgo; system calls run on whatever thread the executor picks), so the
// wiring is not observable and the call has no effect.
func LockOSThread() {}

// UnlockOSThread undoes an earlier call to LockOSThread.
// If this drops the number of active LockOSThread calls on the
// calling goroutine to zero, it unwires the calling goroutine from
// its fixed operating system thread.
// If there are no active LockOSThread calls, this is a no-op.
//
// Before calling UnlockOSThread, the caller must ensure that the OS
// thread is suitable for running other goroutines. If the caller made
// any permanent changes to the state of the thread that would affect
// other goroutines, it should not call this function and thus leave
// the goroutine locked to the OS thread until the goroutine (and
// hence the thread) exits.
func UnlockOSThread() {}

// Breakpoint executes a breakpoint trap.
func Breakpoint() {
	breakpoint()
}

// breakpoint is the trap instruction (an intrinsic: the program dies of
// SIGTRAP, as under gc without a debugger).
func breakpoint()

// NumCgoCall returns the number of cgo calls made by the current process.
//
// graalgo: there is no cgo (CGO_ENABLED=0).
func NumCgoCall() int64 {
	return 0
}

// SetCgoTraceback records three C functions to use to gather
// traceback information from C code and to convert that traceback
// information into symbolic information.
//
// graalgo: there is no cgo; the functions are never called.
func SetCgoTraceback(version int, traceback, context, symbolizer unsafe.Pointer) {}

// SetDefaultGOMAXPROCS updates the GOMAXPROCS setting to the runtime
// default, as described by [GOMAXPROCS], ignoring the GOMAXPROCS
// environment variable.
func SetDefaultGOMAXPROCS() {
	GOMAXPROCS(NumCPU())
}

// A Pinner is a set of Go objects each pinned to a fixed location in memory.
// The [Pinner.Pin] method pins one object, while [Pinner.Unpin] unpins all
// pinned objects.
//
// graalgo: Go objects are JVM objects and nothing outside the JVM holds
// their addresses (system calls copy through native memory), so pinning has
// nothing to do.
type Pinner struct {
	*pinner
}

type pinner struct{}

// Pin pins a Go object, preventing it from being moved or freed by the
// garbage collector until the [Pinner.Unpin] method has been called.
func (p *Pinner) Pin(pointer any) {}

// Unpin unpins all pinned objects of the [Pinner].
func (p *Pinner) Unpin() {}
