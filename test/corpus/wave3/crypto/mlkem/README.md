# `crypto/mlkem` wave-3 corpus

Differential corpus for the crypto/mlkem package (L11, REVIEW-2026-11-18-05).

## Scope boundary

ML-KEM-768/1024 key generation, Encapsulate/Decapsulate round-trip,
tampered-ciphertext implicit rejection, seed round-trip. Keys and
ciphertexts are random; only sizes and shared-secret equality are
pinned.

## Programs

- `00_basic.go` — ML-KEM-768/1024 key generation, Encapsulate/Decapsulate, seed round-trip.

## Retest trigger

Any change to crypto/mlkem key generation, encapsulation,
decapsulation, or key encoding.
