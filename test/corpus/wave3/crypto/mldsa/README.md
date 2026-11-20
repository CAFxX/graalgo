# `crypto/mldsa` wave-3 corpus

Differential corpus for the crypto/mldsa package (L11, REVIEW-2026-11-18-05).

## Scope boundary

ML-DSA-44 key generation from a fixed seed (fully deterministic),
SignDeterministic exact signature pins, Verify round-trip,
tampered-signature and wrong-message/context rejection, seed round-trip,
and MLDSA65/MLDSA87 parameter-set key sizes.

## Programs

- `00_basic.go` — ML-DSA-44 key generation, Sign/Verify, context binding, seed round-trip.

## Retest trigger

Any change to crypto/mldsa key generation, signing, verification, or
key encoding.
