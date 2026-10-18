// SYNTHETIC (Principle 4) — crypto/internal/fips140: synthetic-only support package (not in the user import allow-list); exists to serve the synthetic packages importing it.
// RETIRES WHEN: the synthetic packages importing it retire — then delete this file.
// RE-TEST TRIGGER: re-evaluate when its dependents retire (B-4 phased removal: docs/arch/bridge-inventory.md).
package main

// P3-M9 tier-5: synthetic 'crypto/internal/fips140' package.
const syntheticCryptoFips140Source = `// P3-M9 tier-5: synthetic 'crypto/internal/fips140' root package stub.
// The real package gates FIPS 140-3 mode (GODEBUG fips140=on). In the
// default (non-FIPS) build, Enabled is false and RecordApproved,
// RecordNonApproved, CAST, and PCT are no-ops. This stub pins that
// default behavior; it is behavior-identical to gc's default mode.
// Synthetic-only (not in the user import allow-list).
package fips140

// Enabled reports whether FIPS 140-3 mode is active. Always false here,
// matching gc's default (GODEBUG fips140=off).
var Enabled = false

// RecordApproved is a no-op (FIPS mode disabled).
func RecordApproved() {}

// RecordNonApproved is a no-op (FIPS mode disabled).
func RecordNonApproved() {}

// CAST performs the FIPS power-on self-test hook. No-op here.
func CAST(name string, f func() error) {}

// PCT performs the FIPS pairwise-consistency-test hook. No-op here.
func PCT(name string, f func() error) {}

`
