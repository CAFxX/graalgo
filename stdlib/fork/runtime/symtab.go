// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The exported API of upstream's symtab.go: Frames, Frame and Func.
// Frame is upstream's without its unexported fields; Frames and Func are
// opaque to Go code. The backend reports pcs and decodes them (extern.go:
// callers, funcInfo, pcFileLine); every frame is a logical frame, as the
// backend's stacks have no inlining to undo.

package runtime

// Frames may be used to get function/file/line information for a
// slice of PC values returned by [Callers].
type Frames struct {
	// callers is the slice of PCs that have not yet been expanded to frames.
	callers []uintptr
}

// Frame is the information returned by [Frames] for each call frame.
type Frame struct {
	// PC is the program counter for the location in this frame.
	// For a frame that calls another frame, this will be the
	// program counter of a call instruction. Because of inlining,
	// multiple frames may have the same PC value, but different
	// symbolic information.
	PC uintptr

	// Func is the Func value of this call frame. This may be nil
	// for non-Go code or fully inlined functions.
	Func *Func

	// Function is the package path-qualified function name of
	// this call frame. If non-empty, this string uniquely
	// identifies a single function in the program.
	// This may be the empty string if not known.
	// If Func is not nil then Function == Func.Name().
	Function string

	// File and Line are the file name and line number of the
	// location in this frame. For non-leaf frames, this will be
	// the location of a call. These may be the empty string and
	// zero, respectively, if not known. The file name uses
	// forward slashes, even on Windows.
	File string
	Line int

	// Entry point program counter for the function; may be zero
	// if not known. If Func is not nil then Entry ==
	// Func.Entry().
	Entry uintptr
}

// CallersFrames takes a slice of PC values returned by [Callers] and
// prepares to return function/file/line information.
// Do not change the slice until you are done with the [Frames].
func CallersFrames(callers []uintptr) *Frames { return &Frames{callers: callers} }

// Next returns a [Frame] representing the next call frame in the slice
// of PC values. If it has already returned all call frames, Next
// returns a zero [Frame].
//
// The more result indicates whether the next call to Next will return
// a valid [Frame]. It does not necessarily indicate whether this call
// returned one.
//
// See the [Frames] example for idiomatic usage.
func (ci *Frames) Next() (frame Frame, more bool) {
	for len(ci.callers) > 0 {
		// A return pc; the frame is at the call, one before it (upstream).
		pc := ci.callers[0] - 1
		ci.callers = ci.callers[1:]
		name, entry := funcInfo(pc)
		if entry == 0 {
			continue
		}
		file, line := pcFileLine(pc)
		frame = Frame{PC: pc, Func: &Func{entry: entry}, Function: name, File: file, Line: line, Entry: entry}
		break
	}
	return frame, len(ci.callers) > 0
}

// A Func represents a Go function in the running binary.
type Func struct {
	opaque struct{} // unexported field to disallow conversions
	entry  uintptr
}

// FuncForPC returns a *[Func] describing the function that contains the
// given program counter address, or else nil.
//
// If pc represents multiple functions because of inlining, it returns
// the *Func describing the innermost function, but with an entry of
// the outermost function.
func FuncForPC(pc uintptr) *Func {
	_, entry := funcInfo(pc)
	if entry == 0 {
		return nil
	}
	return &Func{entry: entry}
}

// Name returns the name of the function.
func (f *Func) Name() string {
	if f == nil {
		return ""
	}
	name, _ := funcInfo(f.entry)
	return name
}

// Entry returns the entry address of the function.
func (f *Func) Entry() uintptr {
	return f.entry
}

// FileLine returns the file name and line number of the
// source code corresponding to the program counter pc.
// The result will not be accurate if pc is not a program
// counter within f.
func (f *Func) FileLine(pc uintptr) (file string, line int) {
	return pcFileLine(pc)
}
