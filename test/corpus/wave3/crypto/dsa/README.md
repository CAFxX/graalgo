# `crypto/dsa` wave-3 corpus

Differential corpus for the crypto/dsa package (L11, REVIEW-2026-11-18-05).

## Scope boundary

DSA parameter generation (L1024_N160), key generation, Sign/Verify
round-trip, tampered-signature and wrong-hash rejection. Signature bytes
are random; only lengths and booleans are pinned.

## Programs

- `00_basic.go` — DSA parameter/key generation, Sign/Verify, tampered-signature rejection.

## Retest trigger

Any change to crypto/dsa parameter/key generation, signing, or
verification.
