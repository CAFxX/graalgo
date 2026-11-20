# `crypto/pbkdf2` wave-3 corpus

Differential corpus for the crypto/pbkdf2 package (L11, REVIEW-2026-11-18-05).

## Scope boundary

PBKDF2 key derivation (SHA-1/SHA-256/SHA-512): exact derived-key pins,
iteration sensitivity, key-length behavior, invalid-argument errors.

## Programs

- `00_basic.go` — PBKDF2 derivation: RFC vectors, iteration sensitivity, prefix property, argument errors.

## Retest trigger

Any change to crypto/pbkdf2 derived output or argument validation.
