# `crypto/ed25519` wave-3 corpus

Differential corpus for the crypto/ed25519 package (L11, REVIEW-2026-11-18-05).

## Scope boundary

NewKeyFromSeed with a fixed seed, Sign/Verify (deterministic, RFC 8032), Equal, Seed round-trip, tampered message/signature rejection. All values pinned. No .scopelimited programs. Key generation from randomness is out of scope (fixed seed only); ed25519ph is not exercised.

## Programs

- `00_basic.go` — NewKeyFromSeed fixed seed, Sign/Verify, Equal, Seed round-trip, tamper rejection.

## Retest trigger

ed25519 signing/verification semantics change.
