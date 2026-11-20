# `crypto/des` wave-3 corpus

Differential corpus for the crypto/des package (L11, REVIEW-2026-11-18-05).

## Scope boundary

DES + TripleDES block ciphers: fixed keys, encrypt/decrypt round-trip,
exact ciphertext pins, block sizes, bad-key-size rejection.

## Programs

- `00_basic.go` — DES + TripleDES: fixed-key encrypt/decrypt, exact ciphertext pins, bad key sizes.

## Retest trigger

Any change to crypto/des cipher output, key-size validation, or
BlockSize.
