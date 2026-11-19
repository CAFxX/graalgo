# `crypto/rsa` wave-3 corpus

Differential corpus for the crypto/rsa package (L11, REVIEW-2026-11-18-05).

## Scope boundary

GenerateKey with a seeded math/rand reader (deterministic), PKCS#1 v1.5 sign/verify, OAEP encrypt/decrypt, PKCS#1 marshal/parse round-trip. THIN package: exactly one importer before this program (test/corpus/wave3/crypto/x509/01_parse.go). No .scopelimited programs. Only 1024-bit keys (speed); PSS is out of scope.

## Programs

- `00_basic.go` — Seeded GenerateKey, PKCS#1 v1.5 sign/verify, OAEP, PKCS#1 round-trip.

## Retest trigger

rsa key generation or PKCS#1/OAEP semantics change.
