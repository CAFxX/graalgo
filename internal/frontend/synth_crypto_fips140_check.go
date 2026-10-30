// Retirement: see ../../docs/arch/synth-retirement-tracker.md#cryptointernalfips140check for this package's root cause and retires-when trigger (crypto/internal/fips140/check).
package main

// P3-M9 tier-5: synthetic `crypto/internal/fips140/check` stub.
// The real package does FIPS 140 load-time verification (no-op when
// FIPS is disabled). Empty here. Synthetic-only.
const syntheticCryptoFips140CheckSource = `package check
`
