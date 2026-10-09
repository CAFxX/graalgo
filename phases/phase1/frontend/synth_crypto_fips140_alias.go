package main

// P3-M9 tier-5: synthetic `crypto/internal/fips140/alias` stub.
// The real package uses unsafe.Pointer to detect memory overlap (not
// in frontend/v1 scope). This stub assumes no overlap (the corpus
// never uses overlapping buffers). Synthetic-only.
const syntheticCryptoFips140AliasSource = `package alias

// AnyOverlap reports whether x and y share memory. Always false here.
func AnyOverlap(x, y []byte) bool { return false }

// InexactOverlap reports whether x and y overlap inexactly. Always
// false here.
func InexactOverlap(x, y []byte) bool { return false }
`
