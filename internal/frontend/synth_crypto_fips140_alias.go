package main

// P3-M9 tier-5: synthetic `crypto/internal/fips140/alias` package.
// The real package uses uintptr(unsafe.Pointer(...)) for overlap detection,
// which traps under D5 (GG-0016). Calls to AnyOverlap/InexactOverlap are
// intercepted by the frontend (isAliasIntrinsicFunc) and lowered to the
// "sliceoverlap" wire node (SliceOverlapNode.java), which implements correct
// overlap detection directly on GoSlice values (array identity + range
// intersection). The Go bodies below are never executed and never emitted:
// calls are intercepted, values lower to wrapper closures, and the
// declarations are skipped at import (isSyntheticIntrinsic, R10-09), so
// no funcdecl is ever emitted; the bodies exist for type-checking only.
const syntheticCryptoFips140AliasSource = `package alias

// AnyOverlap reports whether x and y share memory at any (not necessarily
// corresponding) index. Lowered to the sliceoverlap intrinsic.
func AnyOverlap(x, y []byte) bool { return false }

// InexactOverlap reports whether x and y share memory at any non-corresponding
// index. Lowered to the sliceoverlap intrinsic.
func InexactOverlap(x, y []byte) bool { return false }
`
