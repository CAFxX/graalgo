// Package atomicform holds formulations of the amd64 Plan 9 assembly in
// src/internal/runtime/atomic/atomic_amd64.s (the Xadd/Xchg/Cas/And/Or/
// Store/CasRel/StoreRel family).
//
// Strategy: INTRINSIFY. On graalgo these are backend intrinsics; there is
// no Go implementation at all. The mapping target is java.lang.invoke.
// VarHandle (see ../java/AtomicForm.java for the exact per-op mapping
// table, validated by a concurrent stress test).
//
// Why not pure-Go-without-a-lock: Go cannot express atomic memory
// semantics without the very assembly being replaced, so a bare Go
// transcription would be circular. This file instead gives the exact
// SEQUENTIAL semantics of every op (the part the intrinsic must
// implement); atomicity under concurrency is provided here by a striped
// lock that stands in for the intrinsic's hardware atomicity. The lock is
// a channel-based mutex so the formulation needs no imports at all
// (importing sync would be legal but drags the whole sync package into
// what must stay a leaf formulation).
//
// The sequential semantics below are differential-tested single-threaded
// against the real amd64 assembly (randomized op sequences, return values
// and final memory compared); the concurrency properties (no torn
// reads/writes, CAS linearizability) are validated by the Java VarHandle
// stress test, which is the actual formulation the backend will use.
package atomic

import "unsafe"

const nStripes = 64

var stripes [nStripes]chan struct{}

func init() {
	for i := range stripes {
		c := make(chan struct{}, 1)
		c <- struct{}{}
		stripes[i] = c
	}
}

func lockFor(ptr unsafe.Pointer) chan struct{} {
	return stripes[(uintptr(ptr)>>3)%nStripes]
}

func lock(ptr unsafe.Pointer)   { <-lockFor(ptr) }          // acquire: take the token
func unlock(ptr unsafe.Pointer) { lockFor(ptr) <- struct{}{} } // release: return the token

// --- loads (amd64: plain MOV; atomic for aligned addresses) ---

func Load(ptr *uint32) uint32 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	v := *ptr
	return v
}

func Loadp(ptr unsafe.Pointer) unsafe.Pointer {
	lock(ptr)
	defer unlock(ptr)
	v := *(*unsafe.Pointer)(ptr)
	return v
}

func Load64(ptr *uint64) uint64 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	v := *ptr
	return v
}

func LoadAcq(ptr *uint32) uint32      { return Load(ptr) }
func LoadAcq64(ptr *uint64) uint64    { return Load64(ptr) }
func LoadAcquintptr(ptr *uintptr) uintptr { return Loaduintptr(ptr) }
func Load8(ptr *uint8) uint8 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	v := *ptr
	return v
}
func Loadint32(ptr *int32) int32 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	v := *ptr
	return v
}
func Loadint64(ptr *int64) int64 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	v := *ptr
	return v
}
func Loaduint(ptr *uint) uint {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	v := *ptr
	return v
}
func Loaduintptr(ptr *uintptr) uintptr {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	v := *ptr
	return v
}

// --- Xadd family: *ptr += delta, return new value ---

func Xadd(ptr *uint32, delta int32) uint32 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	*ptr += uint32(delta)
	v := *ptr
	return v
}

func Xadd64(ptr *uint64, delta int64) uint64 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	*ptr += uint64(delta)
	v := *ptr
	return v
}

func Xadduintptr(ptr *uintptr, delta uintptr) uintptr {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	*ptr += delta
	v := *ptr
	return v
}

func Xaddint32(ptr *int32, delta int32) int32 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	*ptr += delta
	v := *ptr
	return v
}

func Xaddint64(ptr *int64, delta int64) int64 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	*ptr += delta
	v := *ptr
	return v
}

// --- Xchg family: swap, return old value ---

func Xchg8(ptr *uint8, new uint8) uint8 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	old := *ptr
	*ptr = new
	return old
}

func Xchg(ptr *uint32, new uint32) uint32 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	old := *ptr
	*ptr = new
	return old
}

func Xchg64(ptr *uint64, new uint64) uint64 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	old := *ptr
	*ptr = new
	return old
}

func Xchguintptr(ptr *uintptr, new uintptr) uintptr {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	old := *ptr
	*ptr = new
	return old
}

func Xchgint32(ptr *int32, new int32) int32 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	old := *ptr
	*ptr = new
	return old
}

func Xchgint64(ptr *int64, new int64) int64 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	old := *ptr
	*ptr = new
	return old
}

// --- And/Or family (void) and And/Or+width (return old value) ---

func And8(ptr *uint8, val uint8) {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	*ptr &= val
}

func Or8(ptr *uint8, val uint8) {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	*ptr |= val
}

func And(ptr *uint32, val uint32) {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	*ptr &= val
}

func Or(ptr *uint32, val uint32) {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	*ptr |= val
}

func And32(ptr *uint32, val uint32) uint32 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	old := *ptr
	*ptr &= val
	return old
}

func Or32(ptr *uint32, val uint32) uint32 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	old := *ptr
	*ptr |= val
	return old
}

func And64(ptr *uint64, val uint64) uint64 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	old := *ptr
	*ptr &= val
	return old
}

func Or64(ptr *uint64, val uint64) uint64 {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	old := *ptr
	*ptr |= val
	return old
}

func Anduintptr(ptr *uintptr, val uintptr) uintptr {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	old := *ptr
	*ptr &= val
	return old
}

func Oruintptr(ptr *uintptr, val uintptr) uintptr {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	old := *ptr
	*ptr |= val
	return old
}

// --- Cas family: if *ptr == old { *ptr = new; return true } ---

func Cas(ptr *uint32, old, new uint32) bool {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	ok := *ptr == old
	if ok {
		*ptr = new
	}
	return ok
}

func Cas64(ptr *uint64, old, new uint64) bool {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	ok := *ptr == old
	if ok {
		*ptr = new
	}
	return ok
}

func CasRel(ptr *uint32, old, new uint32) bool { return Cas(ptr, old, new) }

func Casp1(ptr *unsafe.Pointer, old, new unsafe.Pointer) bool {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	ok := *ptr == old
	if ok {
		*ptr = new
	}
	return ok
}

func Casint32(ptr *int32, old, new int32) bool {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	ok := *ptr == old
	if ok {
		*ptr = new
	}
	return ok
}

func Casint64(ptr *int64, old, new int64) bool {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	ok := *ptr == old
	if ok {
		*ptr = new
	}
	return ok
}

func Casuintptr(ptr *uintptr, old, new uintptr) bool {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	ok := *ptr == old
	if ok {
		*ptr = new
	}
	return ok
}

// --- Store family ---

func Store(ptr *uint32, val uint32) {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	*ptr = val
}

func Store8(ptr *uint8, val uint8) {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	*ptr = val
}

func Store64(ptr *uint64, val uint64) {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	*ptr = val
}

func StoreRel(ptr *uint32, val uint32)         { Store(ptr, val) }
func StoreRel64(ptr *uint64, val uint64)       { Store64(ptr, val) }
func StoreReluintptr(ptr *uintptr, val uintptr) { Storeuintptr(ptr, val) }

func Storeint32(ptr *int32, val int32) {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	*ptr = val
}

func Storeint64(ptr *int64, val int64) {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	*ptr = val
}

func Storeuintptr(ptr *uintptr, val uintptr) {
	p := unsafe.Pointer(ptr)
	lock(p)
	defer unlock(p)
	*ptr = val
}

// StorepNoWB performs *ptr = val without a write barrier.
// On graalgo there are no write barriers (JVM GC), so this is a plain
// atomic pointer store: VarHandle.setVolatile.
func StorepNoWB(ptr unsafe.Pointer, val unsafe.Pointer) {
	lock(ptr)
	defer unlock(ptr)
	*(*unsafe.Pointer)(ptr) = val
}

// ---- spike additions: exact-width And/Or and write-barriered pointer ops ----

//go:nosplit
func Andint32(ptr *int32, val int32) int32 {
	return int32(And32((*uint32)(unsafe.Pointer(ptr)), uint32(val)))
}

//go:nosplit
func Orint32(ptr *int32, val int32) int32 {
	return int32(Or32((*uint32)(unsafe.Pointer(ptr)), uint32(val)))
}

//go:nosplit
func Andint64(ptr *int64, val int64) int64 {
	return int64(And64((*uint64)(unsafe.Pointer(ptr)), uint64(val)))
}

//go:nosplit
func Orint64(ptr *int64, val int64) int64 {
	return int64(Or64((*uint64)(unsafe.Pointer(ptr)), uint64(val)))
}

// StorePointerWB stores val into *addr atomically *with* a GC write barrier
// (typed unsafe.Pointer store, so gc emits the barrier). Used by the
// sync/atomic StorePointer / SwapPointer / CompareAndSwapPointer wiring.
func StorePointerWB(addr *unsafe.Pointer, val unsafe.Pointer) {
	p := unsafe.Pointer(addr)
	lock(p)
	defer unlock(p)
	*addr = val
}

// XchgPointer swaps *addr with v atomically, with a write barrier.
func XchgPointer(addr *unsafe.Pointer, v unsafe.Pointer) (old unsafe.Pointer) {
	p := unsafe.Pointer(addr)
	lock(p)
	defer unlock(p)
	old = *addr
	*addr = v
	return old
}

// CasPointerWB compares and swaps *addr atomically, with a write barrier.
func CasPointerWB(addr *unsafe.Pointer, old, new unsafe.Pointer) bool {
	p := unsafe.Pointer(addr)
	lock(p)
	defer unlock(p)
	ok := *addr == old
	if ok {
		*addr = new
	}
	return ok
}

// casPointer is declared bodyless in types.go (its body ships in atomic_amd64.s
// upstream). Formulation with write-barrier semantics.
func casPointer(ptr *unsafe.Pointer, old, new unsafe.Pointer) bool {
	return CasPointerWB(ptr, old, new)
}
