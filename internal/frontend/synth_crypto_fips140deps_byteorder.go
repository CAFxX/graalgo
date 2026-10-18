// SYNTHETIC (Principle 4) — crypto/internal/fips140deps/byteorder: synthetic-only support package (not in the user import allow-list); exists to serve the synthetic packages importing it.
// RETIRES WHEN: the synthetic packages importing it retire — then delete this file.
// RE-TEST TRIGGER: re-evaluate when its dependents retire (B-4 phased removal: docs/arch/bridge-inventory.md).
package main

// P3-M9 tier-5: synthetic 'crypto/internal/fips140deps/byteorder' package.
const syntheticCryptoFips140depsByteorderSource = `// P3-M9 tier-5: synthetic 'crypto/internal/fips140deps/byteorder'.
// go1.27.1 vendored byteorder.go. Synthetic-only.
package byteorder

import (
	"internal/byteorder"
)


// ---- vendored from crypto/internal/fips140deps/byteorder/byteorder.go ----



func LEUint16(b []byte) uint16 {
	return byteorder.LEUint16(b)
}

func BEUint32(b []byte) uint32 {
	return byteorder.BEUint32(b)
}

func BEUint64(b []byte) uint64 {
	return byteorder.BEUint64(b)
}

func LEUint64(b []byte) uint64 {
	return byteorder.LEUint64(b)
}

func BEPutUint16(b []byte, v uint16) {
	byteorder.BEPutUint16(b, v)
}

func BEPutUint32(b []byte, v uint32) {
	byteorder.BEPutUint32(b, v)
}

func BEPutUint64(b []byte, v uint64) {
	byteorder.BEPutUint64(b, v)
}

func LEPutUint16(b []byte, v uint16) {
	byteorder.LEPutUint16(b, v)
}

func LEPutUint64(b []byte, v uint64) {
	byteorder.LEPutUint64(b, v)
}

func BEAppendUint16(b []byte, v uint16) []byte {
	return byteorder.BEAppendUint16(b, v)
}

func BEAppendUint32(b []byte, v uint32) []byte {
	return byteorder.BEAppendUint32(b, v)
}

func BEAppendUint64(b []byte, v uint64) []byte {
	return byteorder.BEAppendUint64(b, v)
}

`
