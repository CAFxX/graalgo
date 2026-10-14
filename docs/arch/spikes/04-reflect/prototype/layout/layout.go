// Package layout implements a from-first-principles Go type layout core.
//
// It computes Sizeof/Alignof/field offsets using only the gc layout
// algorithm (see src/cmd/compile/internal/types/size.go calcStructOffset and
// src/cmd/compile/internal/types2/sizes.go), driven by the *structure* of a
// reflect.Type (field list, element types, kinds) but never by its
// Size()/Align()/Offset values, which serve purely as the differential-test
// oracle.
//
// Language choice: Go, because the oracle is gc itself and reflect gives
// direct access to it. The algorithm is pure arithmetic and language
// agnostic; a Truffle/Java port is mechanical.
package layout

import (
	"reflect"
)

const wordSize = 8
const maxAlign = 8

func roundUp(x, a uintptr) uintptr { return (x + a - 1) &^ (a - 1) }

// isAtomicAlign64 reports whether t is the named type sync/atomic.align64,
// which forces 8-alignment on its containing struct (see IsSyncAtomicAlign64
// in cmd/compile/internal/types2).
func isAtomicAlign64(t reflect.Type) bool {
	return t.Name() == "align64" && t.PkgPath() == "sync/atomic"
}

// SizeOf computes the gc size of t from first principles.
func SizeOf(t reflect.Type) uintptr {
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int8, reflect.Uint8:
		return 1
	case reflect.Int16, reflect.Uint16:
		return 2
	case reflect.Int32, reflect.Uint32, reflect.Float32:
		return 4
	case reflect.Int64, reflect.Uint64, reflect.Float64, reflect.Complex64:
		return 8
	case reflect.Complex128:
		return 16
	case reflect.Int, reflect.Uint, reflect.Uintptr:
		return wordSize
	case reflect.String:
		return 2 * wordSize
	case reflect.Slice:
		return 3 * wordSize
	case reflect.Interface:
		return 2 * wordSize
	case reflect.Ptr, reflect.Chan, reflect.Map, reflect.Func,
		reflect.UnsafePointer:
		return wordSize
	case reflect.Array:
		n := t.Len()
		if n == 0 {
			return 0
		}
		esize := SizeOf(t.Elem())
		if esize == 0 {
			return 0
		}
		stride := roundUp(esize, AlignOf(t.Elem()))
		return stride*uintptr(n-1) + esize
	case reflect.Struct:
		n := t.NumField()
		if n == 0 {
			return 0
		}
		offs := FieldOffsets(t)
		size := offs[n-1] + SizeOf(t.Field(n-1).Type)
		// gc issue 9401: a non-zero-sized struct ending in a zero-sized
		// field gets one extra byte so &last cannot alias the next object.
		if size > 0 && SizeOf(t.Field(n-1).Type) == 0 {
			size++
		}
		return roundUp(size, AlignOf(t))
	}
	panic("layout: unexpected kind " + t.Kind().String())
}

// AlignOf computes the gc alignment of t from first principles.
func AlignOf(t reflect.Type) uintptr {
	switch t.Kind() {
	case reflect.Array:
		return AlignOf(t.Elem())
	case reflect.Struct:
		if t.NumField() == 0 && isAtomicAlign64(t) {
			return 8
		}
		max := uintptr(1)
		for i := 0; i < t.NumField(); i++ {
			if a := AlignOf(t.Field(i).Type); a > max {
				max = a
			}
		}
		return max
	case reflect.Slice, reflect.String, reflect.Interface:
		return wordSize
	case reflect.Complex64:
		return 4
	case reflect.Complex128:
		return 8
	}
	if s := SizeOf(t); s >= 1 {
		if s > maxAlign {
			return maxAlign
		}
		return s
	}
	return 1
}

// FieldOffsets computes per-field offsets of a struct type from first
// principles (gc calcStructOffset).
func FieldOffsets(t reflect.Type) []uintptr {
	n := t.NumField()
	offs := make([]uintptr, n)
	var off uintptr
	for i := 0; i < n; i++ {
		ft := t.Field(i).Type
		off = roundUp(off, AlignOf(ft))
		offs[i] = off
		off += SizeOf(ft)
	}
	return offs
}
