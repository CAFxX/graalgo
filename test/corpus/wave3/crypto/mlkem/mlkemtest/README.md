# `crypto/mlkem/mlkemtest` wave-3 corpus

Differential corpus for the crypto/mlkem/mlkemtest package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Deterministic ML-KEM encapsulation via mlkemtest with fixed randomness:
exact shared-secret pins, ciphertext lengths, Decapsulate agreement,
randomness sensitivity.

## Programs

- `00_basic.go` — Deterministic ML-KEM encapsulation with fixed randomness; exact shared-secret pins.

## Retest trigger

Any change to crypto/mlkem/mlkemtest deterministic encapsulation or
ML-KEM shared-secret derivation.
