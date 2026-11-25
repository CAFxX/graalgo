// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The run-time error types programs name (BUG-0046): upstream's
// TypeAssertionError (error.go) and PanicNilError (panic.go), declarations
// and methods verbatim. The backend raises them: a failed type assertion
// panics with a *TypeAssertionError whose fields are the descriptors of the
// types involved, panic(nil) with a *PanicNilError unless GODEBUG
// panicnil=1. The descriptor queries upstream's Error makes through rtype
// are the two body-less functions at the end, intrinsics of the backend.

package runtime

import "internal/abi"

type _type = abi.Type

// A TypeAssertionError explains a failed type assertion.
type TypeAssertionError struct {
	_interface    *_type
	concrete      *_type
	asserted      *_type
	missingMethod string // one method needed by Interface, missing from Concrete
}

func (*TypeAssertionError) RuntimeError() {}

func (e *TypeAssertionError) Error() string {
	inter := "interface"
	if e._interface != nil {
		inter = typeString(e._interface)
	}
	as := typeString(e.asserted)
	if e.concrete == nil {
		return "interface conversion: " + inter + " is nil, not " + as
	}
	cs := typeString(e.concrete)
	if e.missingMethod == "" {
		msg := "interface conversion: " + inter + " is " + cs + ", not " + as
		if cs == as {
			// provide slightly clearer error message
			if typePkgPath(e.concrete) != typePkgPath(e.asserted) {
				msg += " (types from different packages)"
			} else {
				msg += " (types from different scopes)"
			}
		}
		return msg
	}
	return "interface conversion: " + cs + " is not " + as +
		": missing method " + e.missingMethod
}

// A PanicNilError happens when code calls panic(nil).
//
// Before Go 1.21, programs that called panic(nil) observed recover returning nil.
// Starting in Go 1.21, programs that call panic(nil) observe recover returning a *PanicNilError.
// Programs can change back to the old behavior by setting GODEBUG=panicnil=1.
type PanicNilError struct {
	// This field makes PanicNilError structurally different from
	// any other struct in this package, and the _ makes it different
	// from any struct in other packages too.
	// This avoids any accidental conversions being possible
	// between this struct and some other struct sharing the same fields,
	// like happened in go.dev/issue/56603.
	_ [0]*PanicNilError
}

func (*PanicNilError) Error() string { return "runtime error: panic called with nil argument" }
func (*PanicNilError) RuntimeError() {}

// typeString is upstream's toRType(t).string(): the type's name as the
// runtime prints it.
func typeString(t *_type) string

// typePkgPath is upstream's toRType(t).pkgpath(): the import path of the
// package that declares a defined type ("" for other types).
func typePkgPath(t *_type) string
