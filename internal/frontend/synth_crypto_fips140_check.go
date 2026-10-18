// SYNTHETIC (Principle 4) — crypto/internal/fips140/check: synthetic-only support package (not in the user import allow-list); exists to serve the synthetic packages importing it.
// RETIRES WHEN: the synthetic packages importing it retire — then delete this file.
// RE-TEST TRIGGER: re-evaluate when its dependents retire (B-4 phased removal: docs/arch/bridge-inventory.md).
package main

// P3-M9 tier-5: synthetic `crypto/internal/fips140/check` stub.
// The real package does FIPS 140 load-time verification (no-op when
// FIPS is disabled). Empty here. Synthetic-only.
const syntheticCryptoFips140CheckSource = `package check
`
