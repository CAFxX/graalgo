// Package memform holds pure-Go formulations of the amd64 Plan 9 assembly
// in src/runtime/memmove_amd64.s (memmove) and memclr_amd64.s
// (memclrNoHeapPointers), plus memequal/memcmp semantics.
//
// Strategy: INTRINSIFY (memmove/memclr/memequal) — the gc compiler emits
// calls to these for copy(), string==, and clearing; the graalgo backend
// emits its own intrinsic instead. The formulations are the semantic spec:
// overlap-correct copy, zeroing, and equality. Differential-tested against
// the real assembly as observed through the copy builtin, string ==, and
// bytes.Equal/Compare.
//
// Why pure Go: the semantics are byte-granular memory operations with no
// platform content; a Go loop is the exact spec. (memcmp is included for
// completeness although on amd64 it lives in bytealg's compare_amd64.s.)
package memform

import "unsafe"

// Memmove copies n bytes from from to to, correctly handling overlap.
// Formulation of runtime·memmove (memmove_amd64.s). The asm copies
// forward or backward depending on overlap; the observable contract is
// "as if via a temporary buffer".
func Memmove(to, from unsafe.Pointer, n uintptr) {
	if n == 0 || to == from {
		return
	}
	t, f := uintptr(to), uintptr(from)
	if t < f || t >= f+n {
		// No harmful overlap: copy forward.
		for i := uintptr(0); i < n; i++ {
			*(*byte)(unsafe.Add(to, i)) = *(*byte)(unsafe.Add(from, i))
		}
		return
	}
	// Overlap with to > from: copy backward.
	for i := n; i > 0; i-- {
		*(*byte)(unsafe.Add(to, i-1)) = *(*byte)(unsafe.Add(from, i-1))
	}
}

// MemclrNoHeapPointers zeroes n bytes at ptr.
// Formulation of runtime·memclrNoHeapPointers (memclr_amd64.s). The
// NoHeapPointers variant skips the write barrier; on graalgo there are no
// write barriers at all (JVM GC), so the distinction evaporates and one
// formulation covers memclrNoHeapPointers, memclrHasPointers, and
// typedmemclr's zeroing path.
func MemclrNoHeapPointers(ptr unsafe.Pointer, n uintptr) {
	for i := uintptr(0); i < n; i++ {
		*(*byte)(unsafe.Add(ptr, i)) = 0
	}
}

// Memequal reports whether the n bytes at a and b are equal.
// Formulation of runtime·memequal (equal_amd64.s in internal/bytealg,
// linknamed into runtime).
func Memequal(a, b unsafe.Pointer, n uintptr) bool {
	for i := uintptr(0); i < n; i++ {
		if *(*byte)(unsafe.Add(a, i)) != *(*byte)(unsafe.Add(b, i)) {
			return false
		}
	}
	return true
}

// Memcmp compares the n bytes at a and b lexicographically.
// Formulation of the comparison kernel behind bytealg.Compare.
func Memcmp(a, b unsafe.Pointer, n uintptr) int {
	for i := uintptr(0); i < n; i++ {
		x, y := *(*byte)(unsafe.Add(a, i)), *(*byte)(unsafe.Add(b, i))
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}
