package main

// P3-M9 tier-5: synthetic `crypto/internal/fips140/check` stub.
// The real package does FIPS 140 load-time verification (no-op when
// FIPS is disabled). Verified is false (FIPS disabled in graalgo).
// Synthetic-only.
const syntheticCryptoFips140CheckSource = `package check

// Verified is set when FIPS 140 verification succeeded. Always false
// in graalgo (FIPS disabled); matches the real package's API.
var Verified bool
`
